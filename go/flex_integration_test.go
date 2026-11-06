// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

// Capability probes must clean up their temporary objects without initializing
// driver metadata, whether a deployment meets the stopgap requirements or not.

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func TestFlexProbeNoMetadata(t *testing.T) {
	uri := os.Getenv("REDIS_FLEX_URI")
	if uri == "" {
		t.Skip("set REDIS_FLEX_URI to a Redis Flex database to run this test")
	}
	ctx := context.Background()
	opts, err := goredis.ParseURL(uri)
	if err != nil {
		t.Fatal(err)
	}
	raw := goredis.NewClient(opts)
	defer raw.Close()
	keysBefore, err := raw.DBSize(ctx).Result()
	if err != nil {
		t.Fatal(err)
	}

	st := &store{client: raw}
	err = st.configureSearch(ctx, true)
	if err != nil {
		var ae adbc.Error
		if !errors.As(err, &ae) || ae.Code != adbc.StatusNotImplemented {
			t.Fatal(err)
		}
		t.Logf("target lacks stopgap capabilities: %v", err)
	} else if !st.flex {
		t.Fatal("probe succeeded without enabling bounded execution")
	}

	// No persistent metadata or probe objects may remain.
	if keysAfter, err := raw.DBSize(ctx).Result(); err != nil || keysAfter != keysBefore {
		t.Errorf("database has %d keys after the capability probe, %d before (err %v)", keysAfter, keysBefore, err)
	}
}
