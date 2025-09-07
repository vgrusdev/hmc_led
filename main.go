package main

import (
	"context"
	"log"
	"runtime"

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

	flag "github.com/spf13/pflag"
	"github.com/vgrusdev/hmc_led/internal/config"
)

var (
	// the released version
	version string = "development"
	// the time the binary was built
	buildDate string = "September 2025"
	// global --help flag
	helpFlag *bool
	// global --version flag
	versionFlag *bool
)

func init() {
	flag.String("port", "9680", "The port number to listen on for HTTP requests")
	flag.String("address", "0.0.0.0", "The address to listen on for HTTP requests")
	flag.String("log-level", "info", "The minimum logging level; levels are, in ascending order: debug, info, warn, error")
	flag.String("hmc-name", "", "The name of connected HMC, e.g. HMC1")
	flag.String("hmc-hostname", "hmc.localhost", "The host name of connected HMC api interface. Hrdcored port 12443, e.g. https://host:12443.")
	flag.String("tls-skip-verify", "no", "For HTTPS scheme, should certificates signed by unknown authority being ignored")
	flag.StringP("config", "c", "", "The path to a custom configuration file. NOTE: it must be in yaml format.")
	flag.CommandLine.SortFlags = false

	helpFlag = flag.BoolP("help", "h", false, "show this help message")
	versionFlag = flag.Bool("version", false, "show version and build information")
}

func main() {
	flag.Parse()

	switch {
	case *helpFlag:
		showHelp()
	case *versionFlag:
		showVersion()
	default:
		run()
	}
}

func run() {
	var err error

	globalConfig, err := config.New(flag.CommandLine)
	if err != nil {
		log.Fatalf("Could not initialize config: %s", err)
	}

	fmt.Println("Start program")

	// Handle graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hmc := NewHMC(globalConfig)
	defer hmc.Shutdown()

	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	err = hmc.Logon(reqCtx)
	cancel()
	if err != nil {
		fmt.Println("Logon Error: %v\n", err)
	}

}

func showHelp() {
	flag.Usage()
	os.Exit(0)
}

func showVersion() {
	fmt.Printf("%s version\nbuilt with %s %s/%s %s\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH, buildDate)
	os.Exit(0)
}
