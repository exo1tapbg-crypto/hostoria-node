package system

import (
	"runtime"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// Stats contains a snapshot of the node's resource usage.
type Stats struct {
	Architecture string  `json:"architecture"`
	CPUCount     int     `json:"cpu_count"`
	MemoryTotal  uint64  `json:"memory_total"`
	MemoryUsed   uint64  `json:"memory_used"`
	DiskTotal    uint64  `json:"disk_total"`
	DiskUsed     uint64  `json:"disk_used"`
	OS           string  `json:"os"`
}

// Gather collects a snapshot of node resource usage.
// dataDir is used for the disk usage check; falls back to "/" on failure.
func Gather(dataDir string) (*Stats, error) {
	memStat, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	diskStat, err := disk.Usage(dataDir)
	if err != nil {
		diskStat, err = disk.Usage("/")
		if err != nil {
			diskStat = &disk.UsageStat{}
		}
	}

	cpuCount, _ := cpu.Counts(true)

	return &Stats{
		Architecture: runtime.GOARCH,
		CPUCount:     cpuCount,
		MemoryTotal:  memStat.Total,
		MemoryUsed:   memStat.Used,
		DiskTotal:    diskStat.Total,
		DiskUsed:     diskStat.Used,
		OS:           runtime.GOOS,
	}, nil
}
