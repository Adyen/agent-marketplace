package fsboundary

import "syscall"

func changeTimeNS(st *syscall.Stat_t) int64 {
	return st.Ctim.Nano()
}
