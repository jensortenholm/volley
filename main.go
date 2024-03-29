package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/s3rj1k/go-fanotify/fanotify"
	"golang.org/x/sys/unix"
)

var (
	timers          = make(map[string]*time.Timer)
	sourcePath      string
	destinationPath string
	verbose         bool
	waitTime        int
	waitFor         time.Duration
	mu              sync.Mutex
	callback        = func(path string) {
		fmt.Println("Timer expired, moving file or directory:", path)
		src := filepath.Join(sourcePath, path)
		dst := filepath.Join(destinationPath, path)

		err := os.Rename(src, dst)
		if err != nil {
			fmt.Printf("Error moving %v to %v: %v", src, dst, err)
		}

		mu.Lock()
		delete(timers, path)
		mu.Unlock()
	}
)

func validateExistingDir(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	} else if !stat.IsDir() {
		return false
	}
	return true
}

func main() {
	flag.StringVar(&sourcePath, "src", "", "Source path")
	flag.StringVar(&destinationPath, "dst", "", "Destination path")
	flag.BoolVar(&verbose, "verbose", false, "Verbose logging")
	flag.IntVar(&waitTime, "wait", 120, "Number of seconds to wait for more events before moving files")

	flag.Parse()

	if sourcePath == "" || destinationPath == "" {
		log.Fatalf("Error: source and destination path has to be specified.")
	}

	waitFor = time.Duration(waitTime) * time.Second

	if !validateExistingDir(sourcePath) || !validateExistingDir(destinationPath) {
		log.Fatalf("Error: source and destination path must exist and be directories.")
	}

	fmt.Println("Watching directory", sourcePath, "and moving to", destinationPath)

	notify, err := fanotify.Initialize(
		unix.FAN_CLOEXEC|
			unix.FAN_UNLIMITED_QUEUE|
			unix.FAN_UNLIMITED_MARKS,
		os.O_RDONLY|
			unix.O_LARGEFILE|
			unix.O_CLOEXEC,
	)

	if err != nil {
		log.Fatalf("%v\n", err)
	}

	if err = notify.Mark(
		unix.FAN_MARK_ADD|
			unix.FAN_MARK_MOUNT,
		unix.FAN_MODIFY|
			unix.FAN_CLOSE_WRITE,
		unix.AT_FDCWD,
		sourcePath,
	); err != nil {
		log.Fatalf("%v\n", err)
	}

	for {
		data, err := notify.GetEvent(os.Getpid())
		if err != nil {
			log.Fatalf("Error: %v\n", err)
		}

		if data == nil {
			continue
		}

		path, err := data.GetPath()
		if err != nil {
			log.Fatalf("Error getting path for event. %v\n", err)
		}

		// Filter out events not related to our source path
		if !strings.HasPrefix(path, sourcePath) {
			data.Close()
			continue
		}

		if verbose {
			fmt.Println("Received an event for", path)
		}

		if data.MatchMask(unix.FAN_CLOSE_WRITE) || data.MatchMask(unix.FAN_MODIFY) {
			name := getWatchComponent(path, sourcePath)
			mu.Lock()
			timer, ok := timers[name]
			mu.Unlock()

			// If no timer exists yet, create one
			if !ok {
				fmt.Println("New content detected, watching:", name)
				timer = time.AfterFunc(math.MaxInt64, func() { callback(name) })
				timer.Stop()

				mu.Lock()
				timers[name] = timer
				mu.Unlock()
			}

			// An event was registered, so reset the timer
			if verbose {
				fmt.Println("Received an event, so resetting the timer for", name)
			}
			timer.Reset(waitFor)
		}
		data.Close()
	}
}

// Returns either the filename (if path refers to a file in the root of basePath directory)
// or the first directory component of path relative to basePath (if the path is in a subdirectory of basePath)
func getWatchComponent(path, basePath string) string {
	dir, fname := filepath.Split(path)
	relPath, _ := filepath.Rel(basePath, dir)

	if relPath == "." {
		return fname
	} else {
		return strings.Split(relPath, "/")[0]
	}
}
