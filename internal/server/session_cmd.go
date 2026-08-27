package server

import "sync"

var cmdSerials sync.Map // imei -> uint16

func (s *Server) IsClassic(imei string) bool {
	s.mu.RLock()
	sess := s.sessions[imei]
	s.mu.RUnlock()
	return sessionClassic(sess)
}

func (s *Server) NextCommandSerial(imei string) uint16 {
	var n uint16
	if v, ok := cmdSerials.Load(imei); ok {
		n = v.(uint16)
	}
	n++
	if n == 0 {
		n = 1
	}
	cmdSerials.Store(imei, n)
	return n
}
