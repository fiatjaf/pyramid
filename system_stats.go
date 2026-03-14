package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dustin/go-humanize"
	"github.com/fiatjaf/pyramid/global"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

type SystemStats struct {
	CPUUsage      string
	CPUUsages     []float64
	CPUProcess    string
	Memory        string
	MemoryProcess string
	DataSize      string
	DiskTotal     string
	DiskUsed      string
	DiskUsedPct   string
}

func getSystemStats() SystemStats {
	stats := SystemStats{
		CPUUsage:      "n/a",
		CPUProcess:    "n/a",
		Memory:        "n/a",
		MemoryProcess: "n/a",
		DataSize:      "n/a",
		DiskTotal:     "n/a",
		DiskUsed:      "n/a",
		DiskUsedPct:   "n/a",
	}

	if usages, err := cpu.Percent(0, true); err == nil && len(usages) > 0 {
		stats.CPUUsages = usages
	}
	if totalUsage, err := cpu.Percent(0, false); err == nil && len(totalUsage) > 0 {
		stats.CPUUsage = fmt.Sprintf("%.1f%%", totalUsage[0])
	}

	if memory, err := mem.VirtualMemory(); err == nil && memory.Total > 0 {
		stats.Memory = fmt.Sprintf("%s / %s (%.1f%% used)", humanize.Bytes(memory.Used), humanize.Bytes(memory.Total), memory.UsedPercent)
	}

	if usage, err := disk.Usage(global.S.DataPath); err == nil && usage.Total > 0 {
		stats.DiskTotal = humanize.Bytes(usage.Total)
		stats.DiskUsed = humanize.Bytes(usage.Used)
		stats.DiskUsedPct = fmt.Sprintf("%.1f%%", usage.UsedPercent)
	}

	if size, err := getDirSize(global.S.DataPath); err == nil {
		stats.DataSize = humanize.Bytes(size)
	}

	if proc, err := process.NewProcess(int32(os.Getpid())); err == nil {
		if cpuPct, err := proc.CPUPercent(); err == nil {
			stats.CPUProcess = fmt.Sprintf("%.1f%%", cpuPct)
		}
		if memInfo, err := proc.MemoryInfo(); err == nil && memInfo.RSS > 0 {
			stats.MemoryProcess = humanize.Bytes(memInfo.RSS)
		}
	}

	return stats
}

func getDirSize(path string) (uint64, error) {
	var total uint64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += uint64(info.Size())
		}
		return nil
	})
	return total, err
}
