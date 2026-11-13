// Command visor-e2e-ssh serves SSH on 127.0.0.1 for the Playwright tests, so
// a server tab can be opened against the e2e sandbox. It logs in user
// "e2e" with password "e2e", runs commands as the user running it and
// forwards unix sockets. Its host key is new on every start, so each run
// sees the trust prompt. Never run it anywhere but a test machine.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/remote/remotetest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8767", "address to listen on (127.0.0.1 only)")
	flag.Parse()
	s, err := remotetest.Serve(remotetest.Options{User: "e2e", Password: "e2e"}, *addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("visor-e2e-ssh listening on %s, host key %s\n", s.Addr, remote.Fingerprint(s.HostKey))
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	<-sig
	s.Close()
}
