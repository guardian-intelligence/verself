package objectstorage

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

type s3ErrorResponse struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource,omitempty"`
	RequestID string   `xml:"RequestId,omitempty"`
}

func rewriteS3XMLResponse(operation, requestedBucket, aliasPrefix, host string, body []byte) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	var out bytes.Buffer
	encoder := xml.NewEncoder(&out)
	var stack []string
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			stack = append(stack, typed.Name.Local)
			if err := encoder.EncodeToken(typed); err != nil {
				return nil, err
			}
		case xml.EndElement:
			if err := encoder.EncodeToken(typed); err != nil {
				return nil, err
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			value := string([]byte(typed))
			if rewritten, ok := rewriteXMLValue(operation, stack, requestedBucket, aliasPrefix, host, value); ok {
				value = rewritten
			}
			if err := encoder.EncodeToken(xml.CharData([]byte(value))); err != nil {
				return nil, err
			}
		default:
			if err := encoder.EncodeToken(token); err != nil {
				return nil, err
			}
		}
	}
	if err := encoder.Flush(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func rewriteXMLValue(operation string, stack []string, requestedBucket, aliasPrefix, host, value string) (string, bool) {
	path := strings.Join(stack, "/")
	switch operation {
	case "ListObjectsV2":
		switch path {
		case "ListBucketResult/Name":
			return requestedBucket, true
		case "ListBucketResult/Prefix", "ListBucketResult/Contents/Key", "ListBucketResult/CommonPrefixes/Prefix":
			return stripAliasPrefix(aliasPrefix, value), true
		}
	case "CreateMultipartUpload":
		switch path {
		case "InitiateMultipartUploadResult/Bucket":
			return requestedBucket, true
		case "InitiateMultipartUploadResult/Key":
			return stripAliasPrefix(aliasPrefix, value), true
		}
	case "CompleteMultipartUpload":
		switch path {
		case "CompleteMultipartUploadResult/Bucket":
			return requestedBucket, true
		case "CompleteMultipartUploadResult/Key":
			return stripAliasPrefix(aliasPrefix, value), true
		case "CompleteMultipartUploadResult/Location":
			key := stripAliasPrefix(aliasPrefix, value[strings.LastIndex(value, "/")+1:])
			return fmt.Sprintf("https://%s/%s/%s", host, requestedBucket, key), true
		}
	case "ListMultipartUploads":
		switch path {
		case "ListMultipartUploadsResult/Bucket":
			return requestedBucket, true
		case "ListMultipartUploadsResult/Prefix", "ListMultipartUploadsResult/KeyMarker", "ListMultipartUploadsResult/NextKeyMarker", "ListMultipartUploadsResult/Upload/Key":
			return stripAliasPrefix(aliasPrefix, value), true
		}
	case "ListParts":
		switch path {
		case "ListPartsResult/Bucket":
			return requestedBucket, true
		case "ListPartsResult/Key":
			return stripAliasPrefix(aliasPrefix, value), true
		}
	}
	return value, false
}

func stripAliasPrefix(prefix, value string) string {
	if prefix == "" {
		return value
	}
	return strings.TrimPrefix(value, prefix)
}
