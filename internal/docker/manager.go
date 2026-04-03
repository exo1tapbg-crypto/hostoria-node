package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/hostoria/hostoria-node/internal/server"
)

const (
	labelManaged    = "hostoria.managed"
	labelUUID       = "hostoria.server_uuid"
	labelInstall    = "hostoria.install"
	installTimeout  = 30 * time.Minute
)

// Manager wraps the Docker SDK client.
type Manager struct {
	cli *client.Client
}

// New connects to the local Docker daemon.
func New() (*Manager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("connecting to Docker daemon: %w", err)
	}
	return &Manager{cli: cli}, nil
}

func (m *Manager) Close() error { return m.cli.Close() }

// PullImage pulls image if not available locally, streaming progress to outputFn.
func (m *Manager) PullImage(ctx context.Context, img string, outputFn func(string)) error {
	out, err := m.cli.ImagePull(ctx, img, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling image %q: %w", img, err)
	}
	defer out.Close()
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		if outputFn != nil {
			outputFn(scanner.Text())
		}
	}
	return scanner.Err()
}

// CreateContainer builds and registers a Docker container for a game server.
// The container is NOT started — call StartContainer next.
func (m *Manager) CreateContainer(ctx context.Context, srv *server.Server, dataDir string) (string, error) {
	// Resolve environment variables in startup command
	startupCmd := srv.StartupCommand
	for k, v := range srv.Environment {
		startupCmd = strings.ReplaceAll(startupCmd, "{{"+k+"}}", v)
	}

	env := make([]string, 0, len(srv.Environment))
	for k, v := range srv.Environment {
		env = append(env, k+"="+v)
	}

	// Port bindings
	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}
	for _, p := range srv.Ports {
		proto := p.Protocol
		if proto != "udp" {
			proto = "tcp"
		}
		np := nat.Port(fmt.Sprintf("%d/%s", p.Port, proto))
		ip := p.IP
		if ip == "" {
			ip = "0.0.0.0"
		}
		exposedPorts[np] = struct{}{}
		portBindings[np] = []nat.PortBinding{{HostIP: ip, HostPort: fmt.Sprintf("%d", p.Port)}}
	}

	// Resource limits
	memBytes := int64(srv.Limits.MemoryMB) * 1024 * 1024
	if memBytes <= 0 {
		memBytes = 512 * 1024 * 1024
	}
	cpuPeriod := int64(100_000)
	cpuQuota := int64(math.Max(1, float64(srv.Limits.CPUPercent))) * cpuPeriod / 100

	resp, err := m.cli.ContainerCreate(ctx,
		&container.Config{
			Image:        srv.DockerImage,
			Cmd:          []string{"/bin/sh", "-c", startupCmd},
			Env:          env,
			ExposedPorts: exposedPorts,
			WorkingDir:   "/home/container",
			Labels: map[string]string{
				labelManaged: "true",
				labelUUID:    srv.UUID,
			},
		},
		&container.HostConfig{
			PortBindings: portBindings,
			Binds:        []string{fmt.Sprintf("%s/%s:/home/container", dataDir, srv.UUID)},
			RestartPolicy: container.RestartPolicy{Name: "no"},
			Resources: container.Resources{
				Memory:    memBytes,
				CPUPeriod: cpuPeriod,
				CPUQuota:  cpuQuota,
			},
		},
		&network.NetworkingConfig{},
		nil,
		"hostoria-"+srv.UUID,
	)
	if err != nil {
		return "", fmt.Errorf("creating container for %s: %w", srv.UUID, err)
	}
	return resp.ID, nil
}

// StartContainer starts an already-created container.
func (m *Manager) StartContainer(ctx context.Context, containerID string) error {
	return m.cli.ContainerStart(ctx, containerID, container.StartOptions{})
}

// StopContainer sends SIGTERM and waits up to 30 s before returning.
func (m *Manager) StopContainer(ctx context.Context, containerID string) error {
	timeout := 30
	return m.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

// KillContainer sends SIGKILL immediately.
func (m *Manager) KillContainer(ctx context.Context, containerID string) error {
	return m.cli.ContainerKill(ctx, containerID, "SIGKILL")
}

// RemoveContainer force-removes a container.
func (m *Manager) RemoveContainer(ctx context.Context, containerID string) error {
	return m.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

// IsRunning reports whether containerID is in the running state.
func (m *Manager) IsRunning(ctx context.Context, containerID string) (bool, error) {
	info, err := m.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return info.State.Running, nil
}

// GetContainerID finds the Docker container ID for a Hostoria server UUID.
// Returns ("", nil) when no container exists for that UUID.
func (m *Manager) GetContainerID(ctx context.Context, serverUUID string) (string, error) {
	list, err := m.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", labelUUID+"="+serverUUID)),
	})
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", nil
	}
	return list[0].ID, nil
}

// GetStats reads a single sample of resource usage from a running container.
func (m *Manager) GetStats(ctx context.Context, containerID string) (*server.Resources, error) {
	resp, err := m.cli.ContainerStats(ctx, containerID, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var s types.StatsJSON
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}

	// CPU percentage
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	numCPU := float64(s.CPUStats.OnlineCPUs)
	if numCPU == 0 {
		numCPU = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	cpuPercent := 0.0
	if systemDelta > 0 && cpuDelta > 0 && numCPU > 0 {
		cpuPercent = (cpuDelta / systemDelta) * numCPU * 100.0
	}

	var rxBytes, txBytes uint64
	for _, net := range s.Networks {
		rxBytes += net.RxBytes
		txBytes += net.TxBytes
	}

	return &server.Resources{
		MemoryBytes:    s.MemoryStats.Usage,
		MemoryLimit:    s.MemoryStats.Limit,
		CPUAbsolute:    cpuPercent,
		NetworkRxBytes: rxBytes,
		NetworkTxBytes: txBytes,
	}, nil
}

// Attach attaches to container stdout/stderr, calling outputFn for each line,
// and writing commands received on stdinCh to the container's stdin.
func (m *Manager) Attach(ctx context.Context, containerID string, outputFn func(string), stdinCh <-chan string) error {
	resp, err := m.cli.ContainerAttach(ctx, containerID, container.AttachOptions{
		Stream: true, Stdin: true, Stdout: true, Stderr: true, Logs: false,
	})
	if err != nil {
		return fmt.Errorf("attaching to container %s: %w", containerID, err)
	}
	defer resp.Close()

	// Read output lines in a goroutine
	go func() {
		scanner := bufio.NewScanner(resp.Reader)
		for scanner.Scan() {
			line := scanner.Text()
			// Docker multiplexed stream: 8-byte header before payload
			if len(line) > 8 {
				line = line[8:]
			}
			outputFn(line)
		}
	}()

	// Forward stdin commands
	for {
		select {
		case <-ctx.Done():
			return nil
		case cmd, ok := <-stdinCh:
			if !ok {
				return nil
			}
			if _, err := fmt.Fprintln(resp.Conn, cmd); err != nil {
				return err
			}
		}
	}
}

// RunInstallScript runs the installation script for a server in a temporary container,
// streaming output to outputFn. Blocks until the script completes or times out.
func (m *Manager) RunInstallScript(ctx context.Context, srv *server.Server, dataDir string, install *server.InstallConfig, outputFn func(string)) error {
	img := install.DockerImage
	if img == "" {
		img = srv.DockerImage
	}
	entrypoint := install.Entrypoint
	if entrypoint == "" {
		entrypoint = "/bin/bash"
	}

	if outputFn != nil {
		outputFn("[hostoria] Pulling install image: " + img)
	}
	if err := m.PullImage(ctx, img, nil); err != nil {
		return fmt.Errorf("pulling install image: %w", err)
	}

	resp, err := m.cli.ContainerCreate(ctx,
		&container.Config{
			Image:      img,
			Cmd:        []string{entrypoint, "-c", install.Script},
			WorkingDir: "/mnt/server",
			Labels: map[string]string{
				labelManaged: "true",
				labelUUID:    srv.UUID,
				labelInstall: "true",
			},
		},
		&container.HostConfig{
			Binds: []string{fmt.Sprintf("%s/%s:/mnt/server", dataDir, srv.UUID)},
		},
		nil, nil,
		"hostoria-install-"+srv.UUID,
	)
	if err != nil {
		return fmt.Errorf("creating install container: %w", err)
	}

	if err := m.StartContainer(ctx, resp.ID); err != nil {
		_ = m.RemoveContainer(context.Background(), resp.ID)
		return fmt.Errorf("starting install container: %w", err)
	}

	// Stream logs
	logOut, err := m.cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{
		ShowStdout: true, ShowStderr: true, Follow: true,
	})
	if err == nil {
		sc := bufio.NewScanner(logOut)
		for sc.Scan() {
			line := sc.Text()
			if len(line) > 8 {
				line = line[8:]
			}
			if outputFn != nil {
				outputFn(line)
			}
		}
		_ = logOut.Close()
	}

	// Wait for exit
	waitCtx, waitCancel := context.WithTimeout(context.Background(), installTimeout)
	defer waitCancel()

	waitCh, errCh := m.cli.ContainerWait(waitCtx, resp.ID, container.WaitConditionNotRunning)
	select {
	case result := <-waitCh:
		_ = m.RemoveContainer(context.Background(), resp.ID)
		if result.StatusCode != 0 {
			return fmt.Errorf("install script exited with code %d", result.StatusCode)
		}
	case err := <-errCh:
		_ = m.RemoveContainer(context.Background(), resp.ID)
		return fmt.Errorf("waiting for install container: %w", err)
	case <-waitCtx.Done():
		_ = m.KillContainer(context.Background(), resp.ID)
		_ = m.RemoveContainer(context.Background(), resp.ID)
		return fmt.Errorf("install script timed out after %s", installTimeout)
	}

	return nil
}

// WatchContainer monitors the container and calls exitFn when it stops.
// Runs in its own goroutine — non-blocking.
func (m *Manager) WatchContainer(ctx context.Context, containerID string, exitFn func(exitCode int)) {
	go func() {
		statusCh, errCh := m.cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
		select {
		case result := <-statusCh:
			exitFn(int(result.StatusCode))
		case <-errCh:
			exitFn(-1)
		case <-ctx.Done():
		}
	}()
}

// RestoreContainerMap returns a map of serverUUID → containerID for all
// Hostoria-managed containers that Docker currently knows about.
func (m *Manager) RestoreContainerMap(ctx context.Context) (map[string]string, error) {
	list, err := m.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", labelManaged+"=true")),
	})
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(list))
	for _, c := range list {
		if uuid := c.Labels[labelUUID]; uuid != "" {
			result[uuid] = c.ID
		}
	}
	return result, nil
}

// StreamLogs attaches to container log output (past + future) and calls outputFn for each line.
func (m *Manager) StreamLogs(ctx context.Context, containerID string, outputFn func(string)) error {
	out, err := m.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
		Tail:       "100",
	})
	if err != nil {
		return err
	}
	defer out.Close()

	// Consume using io.Copy to handle docker's multiplexed stream
	pr, pw := io.Pipe()
	go func() {
		_, _ = io.Copy(pw, out)
		_ = pw.Close()
	}()

	sc := bufio.NewScanner(pr)
	for sc.Scan() {
		line := sc.Text()
		if len(line) > 8 {
			line = line[8:]
		}
		outputFn(line)
	}
	return sc.Err()
}
