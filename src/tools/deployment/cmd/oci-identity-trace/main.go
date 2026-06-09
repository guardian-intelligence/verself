package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/verself/deployment-tools/internal/ociidentity"
)

func main() {
	var workload ociidentity.WorkloadCoordinates
	var archivePath string
	flag.StringVar(&workload.Site, "site", "", "site name")
	flag.StringVar(&workload.NomadNamespace, "nomad-namespace", "", "Nomad namespace")
	flag.StringVar(&workload.NomadJob, "nomad-job", "", "Nomad job ID")
	flag.StringVar(&workload.NomadGroup, "nomad-group", "", "Nomad task group name")
	flag.StringVar(&workload.NomadTask, "nomad-task", "", "Nomad task name")
	flag.StringVar(&archivePath, "oci-archive", "", "OCI image-layout tar archive")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "oci-identity-trace: positional arguments are not supported")
		os.Exit(2)
	}

	digests, err := ociidentity.ReadArchiveDigests(archivePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oci-identity-trace: %v\n", err)
		os.Exit(1)
	}
	packet, err := ociidentity.NewPacket(workload, digests)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oci-identity-trace: %v\n", err)
		os.Exit(1)
	}
	body, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "oci-identity-trace: encode packet: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(body))
}
