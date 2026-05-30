package tasks

import (
	"hash/fnv"
	"os"
	"sort"
	"strconv"
	"testing"
)

// integrationTestWeights holds the approximate per-test runtime (seconds) of
// every TestIntegration* top-level function, used to bin-pack the suite into
// balanced shards (see computeShards). Values come from a CI run; a few
// fast/late tests are estimated. Keep this list in sync with the test
// functions: a test not listed here still runs (it falls back to a hash-based
// shard assignment in skipIfNotInShardT), it just isn't accounted for when
// balancing, so add new heavy tests here when CI timings drift.
var integrationTestWeights = map[string]float64{
	"TestIntegrationAclApp":                                 9.1,
	"TestIntegrationAclService":                             5.8,
	"TestIntegrationAppClone":                               12.6,
	"TestIntegrationAppCreateAndDestroy":                    6.0,
	"TestIntegrationAppJsonPropertyAll":                     7.2,
	"TestIntegrationAppLock":                                7.0,
	"TestIntegrationAppsPropertyAll":                        8.0,
	"TestIntegrationBuilderDockerfilePropertyAll":           7.5,
	"TestIntegrationBuilderHerokuishPropertyAll":            7.5,
	"TestIntegrationBuilderLambdaPropertyAll":               7.5,
	"TestIntegrationBuilderNixpacksPropertyAll":             7.5,
	"TestIntegrationBuilderPackPropertyAll":                 7.5,
	"TestIntegrationBuilderPropertyAll":                     10.0,
	"TestIntegrationBuilderRailpackPropertyAll":             7.5,
	"TestIntegrationBuildpacks":                             7.8,
	"TestIntegrationBuildpacksPropertyAll":                  7.8,
	"TestIntegrationBuildsPropertyAll":                      7.2,
	"TestIntegrationCaddyPropertyAll":                       15.2,
	"TestIntegrationCertsApp":                               12.6,
	"TestIntegrationCertsGlobal":                            3.0,
	"TestIntegrationChecksPropertyAll":                      7.5,
	"TestIntegrationChecksToggle":                           6.3,
	"TestIntegrationConfigMultipleKeys":                     6.7,
	"TestIntegrationConfigSetAndUnset":                      6.5,
	"TestIntegrationCronPropertyAll":                        10.9,
	"TestIntegrationDockerOptions":                          6.8,
	"TestIntegrationDomainsAddAndRemove":                    19.2,
	"TestIntegrationDomainsToggle":                          10.0,
	"TestIntegrationExecuteIdempotent":                      5.9,
	"TestIntegrationGetTasksFullWorkflow":                   5.9,
	"TestIntegrationGitAuth":                                0.6,
	"TestIntegrationGitFromArchive":                         40.4,
	"TestIntegrationGitFromImage":                           39.8,
	"TestIntegrationGitPropertyAll":                         15.7,
	"TestIntegrationGitSync":                                7.3,
	"TestIntegrationHaproxyPropertyAll":                     6.7,
	"TestIntegrationHttpAuth":                               8.3,
	"TestIntegrationLetsencrypt":                            6.9,
	"TestIntegrationLetsencryptPropertyAll":                 32.6,
	"TestIntegrationLogsPropertyAll":                        11.9,
	"TestIntegrationMultiTaskWorkflow":                      6.5,
	"TestIntegrationNetworkCreateAndDestroy":                1.2,
	"TestIntegrationNetworkPropertyAll":                     13.8,
	"TestIntegrationNginxPropertyAll":                       249.7,
	"TestIntegrationOpenrestyPropertyAll":                   254.0,
	"TestIntegrationPlanCommandsPopulatedOnDrift":           0.4,
	"TestIntegrationPlanConfigItemizes":                     5.9,
	"TestIntegrationPlanDetectsMissingApp":                  3.0,
	"TestIntegrationPlanDoesNotMutate":                      0.5,
	"TestIntegrationPlanInSyncAfterApply":                   5.9,
	"TestIntegrationPlugin":                                 60.0,
	"TestIntegrationPortsAddAndRemove":                      11.3,
	"TestIntegrationProxyPropertyAll":                       10.0,
	"TestIntegrationProxyToggle":                            7.7,
	"TestIntegrationPsPropertyAll":                          21.3,
	"TestIntegrationPsScale":                                72.7,
	"TestIntegrationPsScaleSkipDeploy":                      6.2,
	"TestIntegrationRegistryAuthApp":                        7.9,
	"TestIntegrationRegistryAuthGlobal":                     0.9,
	"TestIntegrationRegistryAuthPasswordNotInArgs":          6.4,
	"TestIntegrationRegistryPropertyAll":                    12.1,
	"TestIntegrationResourceLimit":                          6.5,
	"TestIntegrationResourceLimitProcessType":               6.7,
	"TestIntegrationResourceReserve":                        6.5,
	"TestIntegrationResourceReserveProcessType":             6.7,
	"TestIntegrationRunExecInputsCapturesFailureOutput":     0.2,
	"TestIntegrationRunExecInputsPopulatesExitCodeAndStdout": 0.2,
	"TestIntegrationSchedulerDockerLocalPropertyAll":        7.7,
	"TestIntegrationSchedulerK3sPropertyAll":                21.3,
	"TestIntegrationSchedulerPropertyAll":                   8.6,
	"TestIntegrationServiceCreateAndDestroy":                2.9,
	"TestIntegrationServiceLinkAndUnlink":                   32.0,
	"TestIntegrationStorageEnsure":                          6.0,
	"TestIntegrationStorageEntry":                           6.0,
	"TestIntegrationStorageMount":                           6.0,
	"TestIntegrationTraefikPropertyAll":                     12.0,
	"TestIntegrationValidateRunsOffline":                    1.0,
}

var (
	// shardIndex is the 1-based shard this process runs (0 = sharding off).
	shardIndex int
	// shardTotal is the number of shards (0 = sharding off).
	shardTotal int
	// shardOf maps a test name to its 1-based shard for the weighted tests.
	shardOf map[string]int
)

func init() {
	idx, err1 := strconv.Atoi(os.Getenv("DOKKU_TEST_SHARD"))
	tot, err2 := strconv.Atoi(os.Getenv("DOKKU_TEST_SHARDS"))
	if err1 != nil || err2 != nil || tot < 1 || idx < 1 || idx > tot {
		return // sharding disabled: run everything
	}
	shardIndex = idx
	shardTotal = tot
	shardOf = computeShards(integrationTestWeights, tot)
}

// computeShards assigns each weighted test to one of n shards using the
// longest-processing-time greedy heuristic: process tests heaviest-first and
// drop each into the currently-lightest shard. Deterministic for a given
// weights map and n. Returns a map of test name to 1-based shard.
func computeShards(weights map[string]float64, n int) map[string]int {
	type item struct {
		name string
		w    float64
	}
	items := make([]item, 0, len(weights))
	for name, w := range weights {
		items = append(items, item{name, w})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].w != items[j].w {
			return items[i].w > items[j].w
		}
		return items[i].name < items[j].name
	})

	loads := make([]float64, n)
	out := make(map[string]int, len(items))
	for _, it := range items {
		lightest := 0
		for b := 1; b < n; b++ {
			if loads[b] < loads[lightest] {
				lightest = b
			}
		}
		loads[lightest] += it.w
		out[it.name] = lightest + 1
	}
	return out
}

// shardForTest returns the 1-based shard a test belongs to. Listed tests use
// the bin-packed assignment; unlisted tests fall back to an FNV hash so they
// still run in exactly one shard.
func shardForTest(name string) int {
	if s, ok := shardOf[name]; ok {
		return s
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return int(h.Sum32()%uint32(shardTotal)) + 1
}

// skipIfNotInShardT skips the test unless it belongs to the shard this process
// is running. When sharding is disabled (DOKKU_TEST_SHARD/DOKKU_TEST_SHARDS
// unset) it is a no-op, so local runs execute the whole suite.
func skipIfNotInShardT(t *testing.T) {
	t.Helper()
	if shardTotal == 0 {
		return
	}
	if got := shardForTest(t.Name()); got != shardIndex {
		t.Skipf("skipping: test assigned to shard %d/%d, running shard %d", got, shardTotal, shardIndex)
	}
}
