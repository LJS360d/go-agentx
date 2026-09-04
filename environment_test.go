// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

//go:build snmpd

// These tests drive a real net-snmp master agent (snmpd) over a local socket.
// They are the end-to-end check that what this library puts on the wire is what
// an actual master agent accepts, and they need the snmpd binary and
// snmpd.conf, so they are kept out of the default `go test ./...` run:
//
//	go test -tags snmpd ./...

package agentx_test

import (
	"io"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/LJS360d/go-agentx"
)

type environment struct {
	client   *agentx.Client
	tearDown func()
}

func setUpTestEnvironment(tb testing.TB) *environment {
	cmd := exec.Command("snmpd", "-Ln", "-f", "-C", "-c", "snmpd.conf")

	stdout, err := cmd.StdoutPipe()
	require.NoError(tb, err)
	go func() {
		io.Copy(os.Stdout, stdout)
	}()

	log.Printf("run: %s", cmd)
	require.NoError(tb, cmd.Start())
	time.Sleep(500 * time.Millisecond)

	client, err := agentx.Dial("tcp", "127.0.0.1:30705",
		agentx.WithLogger(slog.New(slog.NewTextHandler(os.Stdout, nil))),
		agentx.WithTimeout(60*time.Second))
	require.NoError(tb, err)

	return &environment{
		client: client,
		tearDown: func() {
			require.NoError(tb, client.Close())
			require.NoError(tb, cmd.Process.Kill())
		},
	}
}
