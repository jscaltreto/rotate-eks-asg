package main

import (
	"log"
	"os"

	"github.com/complex64/go-utils/pkg/ctxutil"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/tenjin/rotate-eks-asg/internal/pkg/rotator"
)

var (
	groups = kingpin.Arg("groups", "EKS Auto Scaling Groups to rotate. Omit to rotate all ASGs for the current cluster").Strings()
	dryRun = kingpin.Flag("dryrun", "Don't actually rotate nodes, just print what would be rotated").Default("false").Bool()
	limit  = kingpin.Flag("limit", "Only rotate [limit] oldest node(s)").Uint()
)

func init() {
	_ = os.Setenv("AWS_SDK_LOAD_CONFIG", "true")
}

func main() {
	kingpin.Parse()
	ctx, cancel := ctxutil.ContextWithCancelSignals(os.Kill, os.Interrupt)

	r, err := rotator.NewRotator(*dryRun, *limit)
	if err != nil {
		log.Fatal(err)
	}

	defer cancel()
	if len(*groups) > 0 {
		if err := r.RotateAll(ctx, *groups); err != nil {
			log.Fatal(err)
		}
	} else {
		if err := r.RotateForCluster(ctx); err != nil {
			log.Fatal(err)
		}
	}
}
