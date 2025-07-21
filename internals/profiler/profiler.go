package profiler

import (
	"os"
	"runtime"
	"time"
	"runtime/pprof"
	"fmt"

	"github.com/canonical/pebble/internals/logger"
)

var (
	cpuProfile *os.File
	blockProfile *os.File
	deltaStart time.Time
	profMode string
)

func init() {
	profMode = os.Getenv("PROF")
	if profMode == "" {
		profMode = "none"
	}
	fmt.Printf("Profiling Mode: %v\n", profMode)
}

// StartupStartMarker enables profiling before startup for both
// 'startup' and 'all' profiling mode.
func StartupStartMarker() {
	if profMode == "startup" || profMode == "all" {
		start()
	}
}

// StartupStopMarker disables startup profiling if selected.
func StartupStopMarker() {
	if profMode == "startup" {
		stop()
	}
}

// ShutdownStartMarker enables profiling if shutdown profiling is
// selected.
func ShutdownStartMarker() {
	if profMode == "shutdown" {
		start()
	}
}

// ShutdownStopMarker stops profiling if either "shutdown" or "all"
// profiling mode is selected.
func ShutdownStopMarker() {
	if profMode == "shutdown" || profMode == "all" {
		stop()
	}
}

func start() {
    var err error

    // Profile blocked sync primitives.
    runtime.SetBlockProfileRate(1)

    // CPU Profile
    cpuProfile, err = os.Create(fmt.Sprintf("cpu-%s.pprof", profMode))
    if err != nil {
	    logger.Noticef("Cannot create CPU profile file")
	    return
    }
    err = pprof.StartCPUProfile(cpuProfile)
    if err != nil {
	    logger.Noticef("Cannot start CPU profile recording")
	    return
    }
    deltaStart = time.Now()
}

func stop() {
    var err error

    // Blocked Sync Primitives
    blockProfile, err = os.Create(fmt.Sprintf("block-%s.pprof", profMode))
    if err != nil {
	    logger.Noticef("Cannot create Block profile file")
	    return
    }
    pprof.Lookup("block").WriteTo(blockProfile, 0)
    blockProfile.Close()

    // CPU Profiling
    pprof.StopCPUProfile()
    cpuProfile.Close()

    // Stop duration
    elapse := time.Now().Sub(deltaStart)
    logger.Noticef("Time elapse (%s): %.2fs", profMode, elapse.Seconds())
}
