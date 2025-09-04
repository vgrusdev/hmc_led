package main

import (
	"context"
	//"crypto/tls"
	//"encoding/xml"
	"fmt"
	//"io"
	//"net/http"
	"os"
	"os/signal"
	//"sync/atomic"
	"syscall"
	"time"
)

func main() {
	fmt.Println("Start program")
	// Handle graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hmcName := "HMC1"
	hmcHostname := "10.134.17.107"
	user := "vgviewer"
	password := "abc12abc"
	hmc := NewHMC(hmcName, hmcHostname, user, password)

	defer hmc.Shutdown()

	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	err := hmc.Logon(reqCtx)
	cancel()
	if err != nil {
		fmt.Println("Logon Error: %v\n", err)
	}

}

