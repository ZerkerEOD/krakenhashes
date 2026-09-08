package sync

import "testing"

/*
 * The downloads map is the dedup index and deliberately outlives any single
 * sync, so it accumulates every file the agent has ever been asked for.
 * GetDownloadStats used to count the whole map, which made its numbers a
 * statement about the process rather than about the sync that just finished.
 *
 * That matters because those numbers decide what the agent reports to the
 * backend: one failure early in a long-lived agent's life kept failed > 0 for
 * the rest of the process, so every later sync — including ones that fetched
 * everything cleanly — would report as failed.
 */

func newTestManager() *DownloadManager {
	return &DownloadManager{
		downloads:     make(map[string]*DownloadTask),
		maxConcurrent: 1,
		semaphore:     make(chan struct{}, 1),
		progressChan:  make(chan DownloadProgress, 10),
	}
}

// put seeds a task as if it had been queued during generation gen.
func (dm *DownloadManager) put(key string, status DownloadStatus, gen uint64) {
	dm.downloads[key] = &DownloadTask{Status: status, Generation: gen}
}

func TestGetDownloadStats_ScopedToCurrentBatch(t *testing.T) {
	dm := newTestManager()

	// Batch 1: two files, one of which failed.
	dm.BeginBatch()
	dm.put("wordlist/a.txt", DownloadStatusCompleted, dm.generation)
	dm.put("wordlist/b.txt", DownloadStatusFailed, dm.generation)

	total, _, _, completed, failed := dm.GetDownloadStats()
	if total != 2 || completed != 1 || failed != 1 {
		t.Fatalf("batch 1: total=%d completed=%d failed=%d, want 2/1/1", total, completed, failed)
	}

	// Batch 2: a single clean file. The earlier failure must not follow it.
	dm.BeginBatch()
	dm.put("rule/c.rule", DownloadStatusCompleted, dm.generation)

	total, _, _, completed, failed = dm.GetDownloadStats()
	if total != 1 || completed != 1 || failed != 0 {
		t.Errorf("batch 2: total=%d completed=%d failed=%d, want 1/1/0 "+
			"(a stale failure leaking forward makes every later sync report failed)",
			total, completed, failed)
	}
}

/*
 * A file the agent already holds is still part of the batch that asked for it.
 *
 * QueueDownload returns early for an already-present file, so if that path did
 * not stamp the generation, a sync whose files were ALL already on disk would
 * report total=0. The completion check requires total > 0, so it would never
 * fire, the sync would stay in_progress, and the benchmark readiness gate —
 * which holds work precisely while a sync is in_progress — would block that
 * agent until the staleness grace expired.
 */
func TestGetDownloadStats_CachedFilesCountInTheirBatch(t *testing.T) {
	dm := newTestManager()

	dm.BeginBatch()
	dm.put("binary/hashcat.7z", DownloadStatusCompleted, dm.generation)

	// Second sync asks for the same file, which is already on disk.
	dm.BeginBatch()
	dm.downloads["binary/hashcat.7z"].Generation = dm.generation // what QueueDownload's early return does

	total, pending, downloading, completed, _ := dm.GetDownloadStats()
	if total == 0 {
		t.Fatal("an all-cached batch reported zero files; completion would never be sent " +
			"and the agent would stay in_progress forever")
	}
	if total != 1 || completed != 1 {
		t.Errorf("total=%d completed=%d, want 1/1", total, completed)
	}
	// This is the condition the completion check actually evaluates.
	if !(pending == 0 && downloading == 0 && total > 0) {
		t.Errorf("completion condition not satisfied: pending=%d downloading=%d total=%d",
			pending, downloading, total)
	}
}

// Callers that never open a batch must keep the old whole-map behaviour rather
// than silently reporting nothing.
func TestGetDownloadStats_NoBatchCountsEverything(t *testing.T) {
	dm := newTestManager()
	dm.put("wordlist/a.txt", DownloadStatusCompleted, 0)
	dm.put("wordlist/b.txt", DownloadStatusFailed, 0)

	total, _, _, completed, failed := dm.GetDownloadStats()
	if total != 2 || completed != 1 || failed != 1 {
		t.Errorf("generation 0: total=%d completed=%d failed=%d, want 2/1/1", total, completed, failed)
	}
}
