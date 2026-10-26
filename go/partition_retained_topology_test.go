// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import "testing"

func TestRetainedSourceTopologyIdentityAndMigration(t *testing.T) {
	first := "node-a 127.0.0.1:7000@17000 myself,master - 10 20 3 connected 0-5460\n"
	second := "node-a 127.0.0.1:7000@17000 myself,master - 100 200 3 connected 0-5460\n"
	a, err := retainedSelfTopology(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := retainedSelfTopology(second)
	if err != nil || a != b {
		t.Fatal("clock traffic changed routing identity", err)
	}
	for _, bad := range []string{
		"node-a host myself,master - 10 20 3 connected 0-5460 [42->-node-b]\n",
		"node-a host myself,master - 10 20 3 connected 0-5460 [42-<-node-b]\n",
		"node-a host myself,slave node-b 10 20 3 connected\n",
		"node-a host myself,master,fail - 10 20 3 disconnected 0-5460\n",
		"node-a host master - 10 20 3 connected 0-5460\n",
	} {
		if _, err := retainedSelfTopology(bad); err == nil {
			t.Fatal("unsafe/unknown topology admitted")
		}
	}
	changed, err := retainedSelfTopology("node-a host myself,master - 10 20 4 connected 0-5460\n")
	if err != nil || changed == a {
		t.Fatal("routing epoch change was hidden", err)
	}
}
