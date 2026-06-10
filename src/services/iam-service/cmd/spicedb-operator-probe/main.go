// spicedb-operator-probe reads relationships for an object type, optionally
// writes one relationship. Operator tracer bullet for recovery work.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/authzed-go/v1"
	"github.com/authzed/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	endpoint := flag.String("endpoint", "127.0.0.1:24408", "spicedb grpc endpoint")
	pskFile := flag.String("psk-file", "", "preshared key file")
	readType := flag.String("read-type", "", "object type to read relationships for")
	write := flag.String("write", "", "relationship to write: resourceType:id#relation@subjectType:id")
	deleteRel := flag.String("delete", "", "relationship to delete: resourceType:id#relation@subjectType:id")
	flag.Parse()

	psk, err := os.ReadFile(*pskFile)
	if err != nil {
		fatal(err)
	}
	client, err := authzed.NewClient(*endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpcutil.WithInsecureBearerToken(strings.TrimSpace(string(psk))),
	)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if *readType != "" {
		stream, err := client.ReadRelationships(ctx, &v1.ReadRelationshipsRequest{
			Consistency:        &v1.Consistency{Requirement: &v1.Consistency_FullyConsistent{FullyConsistent: true}},
			RelationshipFilter: &v1.RelationshipFilter{ResourceType: *readType},
		})
		if err != nil {
			fatal(err)
		}
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				fatal(err)
			}
			r := resp.Relationship
			sub := r.Subject.Object.ObjectType + ":" + r.Subject.Object.ObjectId
			if r.Subject.OptionalRelation != "" {
				sub += "#" + r.Subject.OptionalRelation
			}
			fmt.Printf("%s:%s#%s@%s\n", r.Resource.ObjectType, r.Resource.ObjectId, r.Relation, sub)
		}
	}

	for op, spec := range map[v1.RelationshipUpdate_Operation]string{
		v1.RelationshipUpdate_OPERATION_TOUCH:  *write,
		v1.RelationshipUpdate_OPERATION_DELETE: *deleteRel,
	} {
		if spec == "" {
			continue
		}
		resPart, subPart, ok := strings.Cut(spec, "@")
		if !ok {
			fatal(fmt.Errorf("write must be resource#relation@subject"))
		}
		resObj, relation, ok := strings.Cut(resPart, "#")
		if !ok {
			fatal(fmt.Errorf("resource must be type:id#relation"))
		}
		resType, resID, _ := strings.Cut(resObj, ":")
		subObj, subRel, _ := strings.Cut(subPart, "#")
		subType, subID, _ := strings.Cut(subObj, ":")
		rel := &v1.Relationship{
			Resource: &v1.ObjectReference{ObjectType: resType, ObjectId: resID},
			Relation: relation,
			Subject:  &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: subType, ObjectId: subID}, OptionalRelation: subRel},
		}
		resp, err := client.WriteRelationships(ctx, &v1.WriteRelationshipsRequest{
			Updates: []*v1.RelationshipUpdate{{Operation: op, Relationship: rel}},
		})
		if err != nil {
			fatal(err)
		}
		fmt.Println(op.String(), "at", resp.WrittenAt.Token)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
