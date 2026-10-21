package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/rmarkiewicz/android-companion/internal/app"
)

var (
	grpcPort = flag.Int("grpc-port", 8554, "emulator gRPC bridge port")
	avdName  = flag.String("avd", "", "AVD name to launch (skips the in-app picker)")
	serial   = flag.String("serial", "", "adb serial of a connected device to open (skips the in-app picker)")
	wipeData = flag.Bool("wipe", false, "wipe emulator data on boot")
)

func main() {
	flag.Parse()
	if *avdName != "" && *serial != "" {
		fmt.Fprintln(os.Stderr, "error: use either -avd or -serial, not both")
		os.Exit(2)
	}

	cfg := app.Config{
		GRPCPort: *grpcPort,
		AVDName:  *avdName,
		Serial:   *serial,
		WipeData: *wipeData,
	}
	if err := app.Run(context.Background(), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
