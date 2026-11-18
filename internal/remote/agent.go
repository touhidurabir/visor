package remote

import (
	"bytes"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// agentAuth offers the keys an ssh-agent holds. The returned func closes the
// agent connection once the login is done.
func agentAuth(socket string) (ssh.AuthMethod, func(), error) {
	c, err := net.Dial("unix", expandHome(socket))
	if err != nil {
		return nil, func() {}, err
	}
	ag := agent.NewClient(c)
	return ssh.PublicKeysCallback(ag.Signers), func() { _ = c.Close() }, nil
}

// agentHolds reports whether the agent at socket holds the public key in
// pubPath (a key file's .pub); false when either can't be read.
func agentHolds(socket, pubPath string) bool {
	b, err := os.ReadFile(expandHome(pubPath))
	if err != nil {
		return false
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey(b)
	if err != nil {
		return false
	}
	c, err := net.Dial("unix", expandHome(socket))
	if err != nil {
		return false
	}
	defer func() { _ = c.Close() }()
	keys, err := agent.NewClient(c).List()
	if err != nil {
		return false
	}
	for _, k := range keys {
		if bytes.Equal(k.Blob, pub.Marshal()) {
			return true
		}
	}
	return false
}

// DefaultAgentSocket is $SSH_AUTH_SOCK, or empty.
func DefaultAgentSocket() string { return os.Getenv("SSH_AUTH_SOCK") }
