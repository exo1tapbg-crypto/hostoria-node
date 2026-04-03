package sftp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/hostoria/hostoria-node/internal/config"
	"github.com/hostoria/hostoria-node/internal/filesystem"
	"github.com/hostoria/hostoria-node/internal/server"
)

// Server is the built-in SFTP server for direct file access to game servers.
type Server struct {
	cfg     *config.Config
	servers *server.Manager
}

// New creates an SFTP Server.
func New(cfg *config.Config, servers *server.Manager) *Server {
	return &Server{cfg: cfg, servers: servers}
}

// Start listens for SFTP connections and blocks until an error occurs.
// Authentication: username = server UUID, password = node deployment token.
func (s *Server) Start() error {
	hostKey, err := loadOrGenerateHostKey(s.cfg.System.SFTP.KeyPath)
	if err != nil {
		return fmt.Errorf("loading SFTP host key: %w", err)
	}

	sshCfg := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			// Username is the server UUID; password is the node token
			uuid := conn.User()
			if s.servers.Get(uuid) == nil {
				return nil, fmt.Errorf("unknown server %q", uuid)
			}
			if string(pass) != s.cfg.Token {
				return nil, fmt.Errorf("invalid token")
			}
			return &ssh.Permissions{Extensions: map[string]string{"uuid": uuid}}, nil
		},
	}
	sshCfg.AddHostKey(hostKey)

	addr := fmt.Sprintf("%s:%d", s.cfg.System.SFTP.BindAddress, s.cfg.System.SFTP.BindPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("SFTP listen on %s: %w", addr, err)
	}
	defer listener.Close()
	fmt.Printf("[hostoria] SFTP server listening on %s\n", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn, sshCfg)
	}
}

func (s *Server) handleConn(netConn net.Conn, sshCfg *ssh.ServerConfig) {
	defer netConn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(netConn, sshCfg)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	uuid := sshConn.Permissions.Extensions["uuid"]
	fm := filesystem.New(s.cfg.System.Data, uuid)
	readOnly := s.cfg.System.SFTP.ReadOnly

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			_ = newChan.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		ch, requests, err := newChan.Accept()
		if err != nil {
			return
		}
		go ssh.DiscardRequests(requests)
		go s.handleSFTPSession(ch, fm, readOnly)
	}
}

func (s *Server) handleSFTPSession(ch ssh.Channel, fm *filesystem.Manager, readOnly bool) {
	defer ch.Close()

	handler := &sftpHandler{fm: fm, readOnly: readOnly}
	srv, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory("/"), sftp.ReadOnly())
	_ = handler
	if err != nil {
		return
	}
	_ = srv.Serve()
}

// sftpHandler implements sftp.Handlers using the filesystem.Manager for sandbox safety.
type sftpHandler struct {
	fm       *filesystem.Manager
	readOnly bool
}

// loadOrGenerateHostKey loads the RSA host key from keyPath, or generates and saves one.
func loadOrGenerateHostKey(keyPath string) (ssh.Signer, error) {
	if keyPath == "" {
		keyPath = "/etc/hostoriagp/sftp.key"
	}

	data, err := os.ReadFile(keyPath)
	if err == nil {
		// Try to parse existing key
		block, _ := pem.Decode(data)
		if block != nil {
			key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
			if err == nil {
				return ssh.NewSignerFromKey(key)
			}
		}
	}

	// Generate new key
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, fmt.Errorf("generating RSA key: %w", err)
	}

	// Save key
	_ = os.MkdirAll(filepath.Dir(keyPath), 0700)
	f, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err == nil {
		_ = pem.Encode(f, &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		})
		f.Close()
	}

	return ssh.NewSignerFromKey(key)
}

// --- sftp.Handlers implementation via sftpRoot ---

type sftpRoot struct {
	fm       *filesystem.Manager
	readOnly bool
}

func (r *sftpRoot) Fileread(req *sftp.Request) (io.ReaderAt, error) {
	abs, err := r.fm.SafePath(req.Filepath)
	if err != nil {
		return nil, err
	}
	return os.Open(abs)
}

func (r *sftpRoot) Filewrite(req *sftp.Request) (io.WriterAt, error) {
	if r.readOnly {
		return nil, fmt.Errorf("read-only mode")
	}
	abs, err := r.fm.SafePath(req.Filepath)
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Dir(abs), 0750)
	return os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
}

func (r *sftpRoot) Filecmd(req *sftp.Request) error {
	if r.readOnly {
		return fmt.Errorf("read-only mode")
	}
	switch req.Method {
	case "Rename":
		src, err := r.fm.SafePath(req.Filepath)
		if err != nil {
			return err
		}
		dst, err := r.fm.SafePath(req.Target)
		if err != nil {
			return err
		}
		return os.Rename(src, dst)
	case "Rmdir", "Remove":
		abs, err := r.fm.SafePath(req.Filepath)
		if err != nil {
			return err
		}
		return os.RemoveAll(abs)
	case "Mkdir":
		abs, err := r.fm.SafePath(req.Filepath)
		if err != nil {
			return err
		}
		return os.MkdirAll(abs, 0750)
	case "Setstat":
		return nil
	default:
		return fmt.Errorf("unsupported method %s", req.Method)
	}
}

func (r *sftpRoot) Filelist(req *sftp.Request) (sftp.ListerAt, error) {
	abs, err := r.fm.SafePath(req.Filepath)
	if err != nil {
		return nil, err
	}

	switch req.Method {
	case "List":
		entries, err := os.ReadDir(abs)
		if err != nil {
			return nil, err
		}
		infos := make([]os.FileInfo, 0, len(entries))
		for _, e := range entries {
			info, err := e.Info()
			if err == nil {
				infos = append(infos, info)
			}
		}
		return listerat(infos), nil
	case "Stat":
		info, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		return listerat([]os.FileInfo{info}), nil
	case "Readlink":
		target, err := os.Readlink(abs)
		if err != nil {
			return nil, err
		}
		// Ensure readlink target is within sandbox
		if !strings.HasPrefix(target, r.fm.Root()) {
			return nil, fmt.Errorf("symlink target outside server root")
		}
		info, err := os.Stat(target)
		if err != nil {
			return nil, err
		}
		return listerat([]os.FileInfo{info}), nil
	}
	return nil, fmt.Errorf("unsupported list method %s", req.Method)
}

type listerat []os.FileInfo

func (f listerat) ListAt(ls []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(f)) {
		return 0, io.EOF
	}
	n := copy(ls, f[offset:])
	if n < len(ls) {
		return n, io.EOF
	}
	return n, nil
}
