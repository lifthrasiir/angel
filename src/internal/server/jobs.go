package server

import (
	"log"
	"sync"
	"time"

	"github.com/lifthrasiir/angel/internal/database"
	. "github.com/lifthrasiir/angel/internal/types"
)

var (
	housekeepingTicker *time.Ticker
	housekeepingMutex  sync.Mutex
	housekeepingFinish func()
)

// StartHousekeepingJobs starts all provided housekeeping jobs
func StartHousekeepingJobs(jobs []HousekeepingJob) {
	housekeepingMutex.Lock()
	defer housekeepingMutex.Unlock()

	// Stop existing ticker if running
	if housekeepingTicker != nil {
		housekeepingTicker.Stop()
	}

	// Create ticker to run jobs every 10 minutes
	housekeepingTicker = time.NewTicker(10 * time.Minute)

	go func() {
		for _, job := range jobs {
			if err := job.First(); err != nil {
				log.Printf("Housekeeping job %q failed: %v", job.Name(), err)
			}
		}

		log.Printf("Housekeeping jobs started")
	}()

	go func() {
		defer housekeepingTicker.Stop()

		for range housekeepingTicker.C {
			for _, job := range jobs {
				if err := job.Sometimes(); err != nil {
					log.Printf("Housekeeping job %q failed: %v", job.Name(), err)
				}
			}
		}
	}()

	housekeepingFinish = func() {
		for _, job := range jobs {
			if err := job.Last(); err != nil {
				log.Printf("Housekeeping job %q failed: %v", job.Name(), err)
			}
		}

		log.Printf("Housekeeping jobs finished")
	}
}

// StopHousekeepingJobs stops the housekeeping jobs ticker
func StopHousekeepingJobs() {
	housekeepingMutex.Lock()
	defer housekeepingMutex.Unlock()

	if housekeepingTicker != nil {
		housekeepingTicker.Stop()
		housekeepingTicker = nil
	}

	if housekeepingFinish != nil {
		housekeepingFinish()
		housekeepingFinish = nil
	}
}

type tempSessionCleanupJob struct {
	db             *database.Database
	olderThan      time.Duration
	sandboxBaseDir string
}

func (job *tempSessionCleanupJob) Name() string { return "Temporary session cleanup" }
func (job *tempSessionCleanupJob) First() error { return job.Sometimes() }
func (job *tempSessionCleanupJob) Sometimes() error {
	return database.CleanupOldTemporarySessions(job.db, job.olderThan, job.sandboxBaseDir)
}
func (job *tempSessionCleanupJob) Last() error { return nil }
