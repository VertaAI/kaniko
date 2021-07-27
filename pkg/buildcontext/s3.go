/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package buildcontext

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws/request"
	"os"
	"path/filepath"
	"strings"
	"time"

	kConfig "github.com/GoogleContainerTools/kaniko/pkg/config"
	"github.com/GoogleContainerTools/kaniko/pkg/constants"
	"github.com/GoogleContainerTools/kaniko/pkg/util"
	"github.com/GoogleContainerTools/kaniko/pkg/util/bucket"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	signer "github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

// S3 unifies calls to download and unpack the build context.
type S3 struct {
	context string
}

// UnpackTarFromBuildContext download and untar a file from s3
func (s *S3) UnpackTarFromBuildContext() (string, error) {
	bucket, item, err := bucket.GetNameAndFilepathFromURI(s.context)
	if err != nil {
		return "", fmt.Errorf("getting bucketname and filepath from context: %w", err)
	}

	option := session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}
	endpoint := os.Getenv(constants.S3EndpointEnv)
	forcePath := false
	if strings.ToLower(os.Getenv(constants.S3ForcePathStyle)) == "true" {
		forcePath = true
	}
	if endpoint != "" {
		option.Config = aws.Config{
			Endpoint:         aws.String(endpoint),
			S3ForcePathStyle: aws.Bool(forcePath),
			DisableSSL:       aws.Bool(true),
			Credentials:      credentials.NewStaticCredentials(os.Getenv(constants.S3StaticAccessKey), os.Getenv(constants.S3StaticSecret), ""),
		}
	}
	sess, err := session.NewSessionWithOptions(option)
	if err != nil {
		return bucket, err
	}

	s3Client := s3.New(sess)
	if os.Getenv(constants.S3Host) != "" {
		sig := signer.NewSigner(credentials.NewStaticCredentials(os.Getenv(constants.S3StaticAccessKey), os.Getenv(constants.S3StaticSecret), ""))

		//s3Client.Handlers.Sign.Clear()
		s3Client.Handlers.Sign.PushBack(func(request *request.Request) {
			originalHost := request.HTTPRequest.Host
			originalHost2 := request.HTTPRequest.URL.Host
			defer func() {
				request.HTTPRequest.Host = originalHost
				request.HTTPRequest.URL.Host = originalHost2
			}()
			request.HTTPRequest.Host = os.Getenv(constants.S3Host)
			request.HTTPRequest.URL.Host = os.Getenv(constants.S3Host)
			region := "us-east-1"
			if os.Getenv("AWS_REGION") != "" {
				region = os.Getenv("AWS_REGION")
			}
			t := time.Now()
			_, err := sig.Sign(request.HTTPRequest, request.Body, "s3", region, t)
			if err != nil {
				panic(err)
				return
			}
		})
	}
	downloader := s3manager.NewDownloaderWithClient(s3Client)
	directory := kConfig.BuildContextDir
	tarPath := filepath.Join(directory, constants.ContextTar)
	if err := os.MkdirAll(directory, 0750); err != nil {
		return directory, err
	}
	file, err := os.Create(tarPath)
	if err != nil {
		return directory, err
	}
	_, err = downloader.Download(file,
		&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(item),
		})
	if err != nil {
		return directory, err
	}

	return directory, util.UnpackCompressedTar(tarPath, directory)
}
