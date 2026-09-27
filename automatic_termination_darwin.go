package main

/*
#cgo LDFLAGS: -framework Foundation
void nebula_disable_automatic_termination(void);
*/
import "C"

// preventAutomaticTermination keeps this long-running tray process alive when
// it has no visible windows. The matching enable call is intentionally omitted:
// automatic termination must remain disabled for the process's entire lifetime.
func preventAutomaticTermination() {
	C.nebula_disable_automatic_termination()
}
