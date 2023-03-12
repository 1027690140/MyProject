package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/sanity-io/litter"
)

// Scheduler info.

//go:generate easytags $GOFILE json
type Scheduler struct {
	AppID  string `json:"app_id,omitempty"`
	Env    string `json:"env"`
	Zones  []Zone `json:"zones"` // zone-ratio
	Remark string `json:"remark"`
}

// Scheduler info.
type scheduler struct {
	schedulers map[string]*Scheduler
	mutex      sync.RWMutex
	r          *Registry
}

func newScheduler(r *Registry) *scheduler {
	return &scheduler{
		schedulers: make(map[string]*Scheduler),
		r:          r,
	}
}

// Load load scheduler info.
func (s *scheduler) Load(conf []byte) {
	schs := make([]*Scheduler, 0)
	err := json.Unmarshal(conf, &schs)
	if err != nil {
		fmt.Errorf("load scheduler  info  err %v", err)
	}
	for _, sch := range schs {
		s.schedulers[getKey(sch.AppID, sch.Env)] = sch
	}
}

// Load load scheduler info.
func (s *scheduler) Build(schs map[string]*Scheduler) {
	litter.Dump(schs)

	if len(schs) == 0 {
		fmt.Errorf("schemuler is nil: %v", errors.New("schemuler is nil "))
		return
	}
	for _, sch := range schs {
		s.schedulers[getKey(sch.AppID, sch.Env)] = sch
	}
	litter.Dump(s.schedulers)
}

// TODO:dynamic reload scheduler config.
// func (s *scheduler)Reolad(){
//
//}

// Get get scheduler info.
func (s *scheduler) Get(AppID, env string) *Scheduler {
	s.mutex.RLock()
	sch := s.schedulers[getKey(AppID, env)]
	s.mutex.RUnlock()
	return sch
}
