// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"github.com/apache/arrow-adbc/go/adbc"
	"testing"
)

// Catalog-only discovery and nonmatching filters must validate a requested view
// even when no schema/table enumeration callbacks are needed.
func TestSharedSnapshotUnavailableObjects(t *testing.T) {
	for _, failure := range []string{"lease-loss", "expiry"} {
		t.Run(failure, func(t *testing.T) {
			h, raw, _ := newSnapshotHarness(t)
			c := sharedSnapshotConnection(t, h)
			if err := c.SetOption(h.ctx, OptionStringReadSnapshotAction, "acquire"); err != nil {
				t.Fatal(err)
			}
			defer c.SetOption(h.ctx, OptionStringReadSnapshotAction, "release")
			v := c.store.sharedView
			if failure == "lease-loss" {
				if err := raw.HDel(h.ctx, snapshotCatalogKey, "p:"+v.pin.token).Err(); err != nil {
					t.Fatal(err)
				}
			} else {
				v.cancel()
			}
			for _, depth := range []adbc.ObjectDepth{adbc.ObjectDepthCatalogs, adbc.ObjectDepthDBSchemas, adbc.ObjectDepthTables, adbc.ObjectDepthAll} {
				for _, catalog := range []*string{nil, ptr("nonexistent")} {
					r, err := h.conn.GetObjects(h.ctx, depth, catalog, nil, nil, nil, nil)
					if r != nil {
						r.Release()
					}
					if err == nil {
						t.Errorf("unavailable view succeeded at depth %d, catalog filter %v", depth, catalog)
					}
				}
			}
		})
	}
}
