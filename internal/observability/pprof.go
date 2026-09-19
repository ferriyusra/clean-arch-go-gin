package observability

import (
	"net/http"
	"net/http/pprof"
)

// PprofPrefix is the path every profiling handler lives under.
const PprofPrefix = "/debug/pprof/"

// RegisterPprof mounts the stdlib profiling handlers on mux.
//
// The handlers are registered by hand rather than by relying on the
// net/http/pprof package's init(), which registers them on
// http.DefaultServeMux. That side effect is invisible at the call site: it
// would expose every profile on any listener that happens to serve
// DefaultServeMux, including one added years later by someone who never read
// this file. Importing the package for its handler functions and mounting them
// on a mux we control makes the exposure explicit and revocable.
//
// pprof.Index also serves the runtime/pprof named profiles (goroutine, heap,
// allocs, threadcreate, block, mutex) as /debug/pprof/<name>, so they need no
// registration of their own.
func RegisterPprof(mux *http.ServeMux) {
	mux.HandleFunc(PprofPrefix, pprof.Index)
	mux.HandleFunc(PprofPrefix+"cmdline", pprof.Cmdline)
	mux.HandleFunc(PprofPrefix+"profile", pprof.Profile)
	mux.HandleFunc(PprofPrefix+"symbol", pprof.Symbol)
	mux.HandleFunc(PprofPrefix+"trace", pprof.Trace)
}
