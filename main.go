package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
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
	flag.String("srv_port", "9680", "The port number to listen on for HTTP requests")
	flag.String("srv_addr", "0.0.0.0", "The address to listen on for HTTP requests")
	flag.String("log_level", "info", "The minimum logging level; levels are, in ascending order: debug, info, warn, error")
	flag.String("hmc_name", "", "The name of connected HMC, e.g. HMC1")
	flag.String("hmc_hostname", "hmc.localhost", "The host name of connected HMC api interface. Hrdcored port 12443, e.g. https://host:12443.")
	flag.String("tls_skip_verify", "no", "For HTTPS scheme, should certificates signed by unknown authority being ignored")
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

	log.Debugln("Program start.")

	// Handle graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Init HMC struct
	hmc := NewHMC(globalConfig)
	defer hmc.CloseIdleConnections()

	// Init http server
	srv := Srv{}
	srv.SrvInit(ctx, globalConfig, hmc)

	// run http server, waiting for chan message in case of server ended.
	chSrv := make(chan error)
	// run srv.ListenAndServe()
	go srv.Run(chSrv)

	// Block until we receive our signal.
	select { // which channel will be unblocked first ?
	case <-ctx.Done():
		log.Warnln("Shutdown signal received, stopping...")

		// Create a deadline to wait for shutdown everything
		ctxShutdown, cancelShutd := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutd()

		var wg sync.WaitGroup

		wg.Add(1)
		go srv.Shutdown(ctxShutdown, &wg)
		wg.Add(1)
		go hmc.Shutdown(ctxShutdown, &wg)

		// wait for srv.shutdown results
		if e, ok := <-chSrv; ok == true {
			if e != nil {
				log.Warnf("Srv Down: %s", e)
			} else {
				log.Infoln("Srv Down: OK")
			}
		}
		wg.Wait()

	case e := <-chSrv: // srv.ListenAndServe ended itself, probably due to error.
		log.Errorf("Server: %s", e)
		// Create a deadline to wait for shutdown timeout.

		ctxShutdown, cancelShutd := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutd()

		var wg sync.WaitGroup

		wg.Add(1)
		go hmc.Shutdown(ctxShutdown, &wg)
		wg.Wait()

	}
}

func showHelp() {
	flag.Usage()
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Endpoints:")
	fmt.Fprintln(os.Stderr, "  GET /health               - Health check (public)")
	fmt.Fprintln(os.Stderr, "  GET /status               - Some statistics (public)")
	fmt.Fprintln(os.Stderr, "  GET /getManagementConsole - raw XML /rest/api/uom/ManagementConsole")
	fmt.Fprintln(os.Stderr, "  GET /quickManagedSystem   - YAML - servers LED status")
	os.Exit(0)
}

func showVersion() {
	fmt.Printf("%s version\nbuilt with %s %s/%s %s\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH, buildDate)
	os.Exit(0)
}
