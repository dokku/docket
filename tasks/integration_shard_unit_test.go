package tasks

import (
	"testing"
)

func TestComputeShardsBalances(t *testing.T) {
	const n = 4
	assignment := computeShards(integrationTestWeights, n)

	if len(assignment) != len(integrationTestWeights) {
		t.Fatalf("computeShards assigned %d tests; want %d", len(assignment), len(integrationTestWeights))
	}

	loads := make([]float64, n)
	counts := make([]int, n)
	var total float64
	for name, w := range integrationTestWeights {
		shard, ok := assignment[name]
		if !ok {
			t.Errorf("test %q not assigned to a shard", name)
			continue
		}
		if shard < 1 || shard > n {
			t.Errorf("test %q assigned to out-of-range shard %d", name, shard)
			continue
		}
		loads[shard-1] += w
		counts[shard-1]++
		total += w
	}

	for i := 0; i < n; i++ {
		if counts[i] == 0 {
			t.Errorf("shard %d is empty", i+1)
		}
	}

	// Every shard's load should be close to the mean. With two ~250s
	// outliers the best achievable max is bounded by the largest single
	// test, so allow generous headroom but still catch gross imbalance.
	mean := total / n
	var maxLoad float64
	for _, l := range loads {
		if l > maxLoad {
			maxLoad = l
		}
	}
	if maxLoad > mean*1.25 {
		t.Errorf("shard imbalance: max load %.0fs exceeds 1.25x mean %.0fs (loads=%v)", maxLoad, mean, loads)
	}
}

func TestShardForTestDeterministic(t *testing.T) {
	// With sharding configured, every weighted test resolves to a stable
	// shard in [1, n] and unlisted tests hash into range too.
	saveOf, saveTotal := shardOf, shardTotal
	defer func() { shardOf, shardTotal = saveOf, saveTotal }()

	shardTotal = 4
	shardOf = computeShards(integrationTestWeights, shardTotal)

	for name := range integrationTestWeights {
		if got := shardForTest(name); got < 1 || got > shardTotal {
			t.Fatalf("shardForTest(%q) = %d, out of range", name, got)
		}
	}

	unlisted := "TestIntegrationSomethingBrandNew"
	a := shardForTest(unlisted)
	b := shardForTest(unlisted)
	if a != b {
		t.Errorf("shardForTest(%q) not deterministic: %d != %d", unlisted, a, b)
	}
	if a < 1 || a > shardTotal {
		t.Errorf("shardForTest(%q) = %d, out of range", unlisted, a)
	}
}
