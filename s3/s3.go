package s3

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"os"
)

// https://github.com/awsdocs/aws-doc-sdk-examples/blob/main/go/example_code/s3/s3_upload_object.go
func Upload(local_file_path string, remote_file_name string, bucketname string) error {
	file, err := os.Open(local_file_path)
	if err != nil {
		return err
	}

	defer file.Close()
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")},
	)

	// http://docs.aws.amazon.com/sdk-for-go/api/service/s3/s3manager/#NewUploader
	uploader := s3manager.NewUploader(sess)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucketname),
		Key:    aws.String(remote_file_name),
		Body:   file,
	})

	if err != nil {
		return err
	}

	return nil
}