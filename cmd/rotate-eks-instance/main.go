package main

import (
	"log"
	"os"

	"github.com/complex64/go-utils/pkg/ctxutil"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/tenjin/rotate-eks-asg/internal/pkg/rotator"
)

var (
	name       = kingpin.Arg("name", "Internal DNS of EKS instance to rotate").Required().String()
	removeNode = kingpin.Flag("remove", "Remove instance, don't provision a replacement").Default("false").Bool()
	dryRun     = kingpin.Flag("dryrun", "Don't actually rotate nodes, just print what would be rotated").Default("false").Bool()
)

func init() {
	_ = os.Setenv("AWS_SDK_LOAD_CONFIG", "true")
}

func main() {
	kingpin.Parse()

	r, err := rotator.NewRotator(*dryRun, 0, "")
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := ctxutil.ContextWithCancelSignals(os.Kill, os.Interrupt)
	defer cancel()
	if err := r.RotateByInternalDNS(ctx, *name, *removeNode); err != nil {
		log.Fatal(err)
	}
}
