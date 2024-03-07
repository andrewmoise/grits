package server

import (
	"log"
	"sync/atomic"
)

type JobDescriptor struct {
	description string
	stage       atomic.Value // Stores string
	completion  atomic.Value // Stores float32
	cancelFunc  atomic.Value // Stores func()
}

func NewJobDescriptor(description string) *JobDescriptor {
	jd := &JobDescriptor{
		description: description,
	}
	jd.stage.Store("Initializing")
	jd.completion.Store(float32(0))
	return jd
}

func (jd *JobDescriptor) SetCancelFunc(fn func()) {
	jd.cancelFunc.Store(fn)
}

func (jd *JobDescriptor) SetStage(stage string) {
	jd.stage.Store(stage)
}

func (jd *JobDescriptor) SetCompletion(completion float32) {
	jd.completion.Store(completion)
}

func (jd *JobDescriptor) Cancel() {
	if cf, ok := jd.cancelFunc.Load().(func()); ok {
		cf()
	}
}

func (jd *JobDescriptor) Description() string {
	return jd.description
}

func (jd *JobDescriptor) Stage() string {
	return jd.stage.Load().(string)
}

func (jd *JobDescriptor) Completion() float32 {
	return jd.completion.Load().(float32)
}

func (s *Server) CreateJobDescriptor(description string) *JobDescriptor {
	jd := NewJobDescriptor(description)
	s.jobsLock.Lock()
	defer s.jobsLock.Unlock()
	s.jobs[jd] = true
	return jd
}

func (s *Server) Done(jd *JobDescriptor) {
	s.jobsLock.Lock()
	defer s.jobsLock.Unlock()
	delete(s.jobs, jd)
}

func (s *Server) ReportJobs() error {
	s.jobsLock.Lock()
	defer s.jobsLock.Unlock()

	for jd := range s.jobs {
		log.Printf("Job: %s, Stage: %s, Completion: %.2f%%", jd.Description(), jd.Stage(), jd.Completion()*100)
	}

	return nil
}
