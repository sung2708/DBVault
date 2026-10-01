package providers

import (
	"context"
	"github.com/sung2708/DBVault/internal/config"
	"testing"
)

func TestMissingCloudCredentialsAndPrefixValidation(t *testing.T) {
	t.Setenv("ABSENT_ACCESS", "")
	t.Setenv("ABSENT_SECRET", "")
	t.Setenv("ABSENT_GOOGLE", "")
	t.Setenv("ABSENT_AZURE", "")
	for _, c := range []config.Storage{{Type: "s3", S3: config.S3{Region: "us-east-1", AccessKeyEnv: "ABSENT_ACCESS", SecretKeyEnv: "ABSENT_SECRET"}}, {Type: "gcs", GCS: config.GCS{CredentialsFileEnv: "ABSENT_GOOGLE"}}, {Type: "azure", Azure: config.Azure{AccountName: "test", AccountKeyEnv: "ABSENT_AZURE"}}, {Type: "s3", S3: config.S3{Prefix: "../escape"}}, {Type: "gcs", GCS: config.GCS{Prefix: "a/../escape"}}, {Type: "azure", Azure: config.Azure{Prefix: "../escape"}}} {
		if _, closeFn, e := Open(context.Background(), c); e == nil {
			closeFn()
			t.Fatal("unsafe or missing credential configuration accepted", c.Type)
		}
	}
}
