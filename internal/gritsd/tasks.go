package gritsd

import (
	"log"
	"time"
)

func (s *Server) AddPeriodicTask(interval time.Duration, task func() error) {
	ticker := time.NewTicker(interval)
	s.taskWg.Add(1)
	go func() {
		defer ticker.Stop()
		defer s.taskWg.Done()
		for {
			select {
			case <-ticker.C:
				err := task()
				if err != nil {
					log.Printf("Periodic task failed: %v\n", err)
				}
			case <-s.taskStop:
				return
			}
		}
	}()
}

func (s *Server) StopPeriodicTasks() {
	close(s.taskStop)
	s.taskWg.Wait()
}
