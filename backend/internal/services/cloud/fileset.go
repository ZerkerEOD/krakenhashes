package cloud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	wsservice "github.com/ZerkerEOD/krakenhashes/backend/internal/services/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

/*
 * JobFileSet is every file a cloud agent needs to run one job, and nothing else.
 *
 * WHY THIS EXISTS
 *
 * On connect, handler.go fires initiateFileSync unconditionally, which asks the
 * agent for its whole inventory and diffs it against getBackendFiles(..., "")
 * — category hard-coded empty, i.e. the ENTIRE verified corpus. A fresh cloud
 * instance has zero files, so the diff is 100%: every wordlist, every rule and
 * every hashcat binary download while a $2-22/hr GPU sits idle, plus Vast.ai
 * ingress billing on top.
 *
 * Worse, client potfiles are served as file_type "wordlist" (handler.go
 * special-cases the potfile.txt suffix), so the unfixed path ships EVERY
 * client's cracked plaintexts to a rented machine whose operator has root.
 *
 * The fix needs no new protocol: FileSyncCommandPayload already carries an
 * explicit list and the agent downloads exactly what it is given without
 * diffing. This type produces that list.
 */
type JobFileSet struct {
	Files      []wsservice.FileInfo
	TotalBytes int64
	// HashlistEstimated is true when the hashlist size had to be estimated
	// (the hashlists table stores no byte size), so disk sizing keeps headroom.
	HashlistEstimated bool
}

// FileSetResolver builds per-job file sets.
type FileSetResolver struct {
	db *db.DB
}

// NewFileSetResolver creates a resolver.
func NewFileSetResolver(database *db.DB) *FileSetResolver {
	return &FileSetResolver{db: database}
}

/*
 * ResolveForAgent returns the file set for whatever job a rented agent was
 * provisioned for.
 *
 * The agent id is resolved to a job through cloud_instances rather than through
 * anything the agent said, for the same reason its cloud identity is: a file
 * set is a list of things the backend is about to hand a machine it does not
 * control, so the job it is scoped to must be one the backend chose.
 *
 * Returns (nil, nil) for an agent with no live instance — an ordinary on-prem
 * agent, or a cloud agent whose instance has already been torn down.
 */
func (r *FileSetResolver) ResolveForAgent(ctx context.Context, agentID int) ([]wsservice.FileInfo, error) {
	var jobID uuid.UUID
	err := r.db.QueryRowContext(ctx, `
		SELECT ci.job_execution_id
		FROM cloud_instances ci
		WHERE ci.agent_id = $1
		  AND ci.job_execution_id IS NOT NULL
		  AND ci.state NOT IN ('terminated','failed')
		ORDER BY ci.created_at DESC
		LIMIT 1`, agentID).Scan(&jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fileset: resolve job for agent %d: %w", agentID, err)
	}

	set, err := r.Resolve(ctx, jobID)
	if err != nil {
		return nil, err
	}
	return set.Files, nil
}

// avgHashBytes estimates one hashline's size. hashlists has no byte-size
// column, so this is the one component we cannot measure exactly. Deliberately
// generous: under-sizing a Vast.ai disk is unrecoverable, because disk is
// immutable after creation.
const avgHashBytes = 128

/*
 * Resolve returns the complete file set for a job.
 *
 * Sources mirror exactly what the dispatcher will later ask for:
 *   - scheduling_units.wordlist_refs / rule_file_refs (which also covers
 *     ephemeral and filtered wordlists, since those are ordinary wordlists rows)
 *   - client wordlists and the client potfile, split out of the wordlist refs
 *   - the job's hashlist
 *   - the hashcat binary
 */
func (r *FileSetResolver) Resolve(ctx context.Context, jobID uuid.UUID) (*JobFileSet, error) {
	set := &JobFileSet{}
	seen := make(map[string]bool)

	// The client this job belongs to, resolved through its hashlist. Every
	// client-scoped ref is checked against it: a ref naming a DIFFERENT client
	// would otherwise resolve happily and ship that client's cracked
	// plaintexts to rented third-party hardware.
	var owningClientID uuid.UUID
	if err := r.db.QueryRowContext(ctx, `
		SELECT h.client_id
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		WHERE je.id = $1`, jobID).Scan(&owningClientID); err != nil {
		return nil, fmt.Errorf("fileset: resolve owning client for job %s: %w", jobID, err)
	}

	add := func(fi wsservice.FileInfo) {
		key := fi.FileType + "|" + fi.Name
		if seen[key] {
			return
		}
		seen[key] = true
		set.Files = append(set.Files, fi)
		set.TotalBytes += fi.Size
	}

	wordlistRefs, err := r.readRefs(ctx, jobID, "wordlist_refs")
	if err != nil {
		return nil, err
	}
	ruleRefs, err := r.readRefs(ctx, jobID, "rule_file_refs")
	if err != nil {
		return nil, err
	}

	for _, ref := range wordlistRefs {
		fi, err := r.resolveWordlistRef(ctx, ref, owningClientID)
		if err != nil {
			return nil, err
		}
		if fi != nil {
			add(*fi)
		}
	}

	for _, ref := range ruleRefs {
		name := strings.TrimPrefix(ref, "rules/")
		var id int
		var md5 string
		var size int64
		err := r.db.QueryRowContext(ctx, `
			SELECT id, COALESCE(md5_hash,''), COALESCE(file_size,0)
			FROM rules WHERE file_name = $1 AND verification_status = 'verified' LIMIT 1`, name).
			Scan(&id, &md5, &size)
		if err != nil {
			debug.Warning("fileset: rule %q not resolvable, agent will fetch on demand: %v", ref, err)
			continue
		}
		add(wsservice.FileInfo{Name: name, MD5Hash: md5, Size: size, FileType: "rule", ID: id})
	}

	// --- hashlist ----------------------------------------------------------
	var hashlistID int64
	var totalHashes, crackedHashes int
	err = r.db.QueryRowContext(ctx, `
		SELECT h.id, COALESCE(h.total_hashes,0), COALESCE(h.cracked_hashes,0)
		FROM job_executions je JOIN hashlists h ON h.id = je.hashlist_id
		WHERE je.id = $1`, jobID).Scan(&hashlistID, &totalHashes, &crackedHashes)
	if err != nil {
		return nil, fmt.Errorf("fileset: resolve hashlist: %w", err)
	}
	remaining := totalHashes - crackedHashes
	if remaining < 0 {
		remaining = 0
	}
	set.HashlistEstimated = true
	add(wsservice.FileInfo{
		Name:     fmt.Sprintf("%d.hash", hashlistID),
		FileType: "hashlist",
		Size:     int64(remaining) * avgHashBytes,
		ID:       int(hashlistID),
	})

	// --- hashcat binary ----------------------------------------------------
	// Sized from the largest active binary rather than resolved per-agent:
	// DetermineBinaryForTask needs an agent, which does not exist yet at
	// provisioning time, and over-sizing disk is the safe direction.
	var binID int
	var binMD5 string
	var binSize int64
	err = r.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(md5_hash,''), COALESCE(file_size,0)
		FROM binary_versions
		WHERE is_active = true AND verification_status = 'verified'
		ORDER BY file_size DESC LIMIT 1`).Scan(&binID, &binMD5, &binSize)
	if err != nil {
		debug.Warning("fileset: no active binary found for disk sizing: %v", err)
	} else {
		add(wsservice.FileInfo{
			Name: fmt.Sprintf("%d", binID), MD5Hash: binMD5, Size: binSize,
			FileType: "binary", ID: binID,
		})
	}

	return set, nil
}

// readRefs pulls one TEXT[] ref column across every scheduling unit of a job.
// Increment layers share identical refs, hence DISTINCT.
func (r *FileSetResolver) readRefs(ctx context.Context, jobID uuid.UUID, column string) ([]string, error) {
	// column is a package-internal constant, never user input.
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT unnest(`+column+`) FROM scheduling_units WHERE parent_job_id = $1`, jobID)
	if err != nil {
		return nil, fmt.Errorf("fileset: read %s: %w", column, err)
	}
	defer rows.Close()

	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, fmt.Errorf("fileset: scan %s: %w", column, err)
		}
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs, rows.Err()
}

// resolveWordlistRef turns one scheduling-unit ref into a FileInfo, routing
// client-scoped paths to their own tables. Returns nil when the ref cannot be
// resolved: the agent's per-task ensure* chain remains the safety net.
func (r *FileSetResolver) resolveWordlistRef(ctx context.Context, ref string, owningClientID uuid.UUID) (*wsservice.FileInfo, error) {
	trimmed := strings.TrimPrefix(ref, "wordlists/")

	// wordlists/clients/{client-uuid}/{file}
	if strings.HasPrefix(trimmed, "clients/") {
		parts := strings.Split(trimmed, "/")
		if len(parts) != 3 {
			return nil, nil
		}
		clientID, filename := parts[1], parts[2]

		// A ref may only name the job's OWN client. Anything else is either a
		// bug in unit construction or an attempt to exfiltrate another
		// engagement's data, and neither should be resolved. This is the last
		// point at which the difference is knowable — everything downstream
		// just downloads what it is handed.
		if clientID != owningClientID.String() {
			debug.Error("fileset: REFUSED cross-client file ref %q: job belongs to client %s. "+
				"This is either a unit-construction bug or an exfiltration attempt.",
				ref, owningClientID)
			return nil, nil
		}

		if filename == "potfile.txt" {
			var size int64
			var md5 string
			if err := r.db.QueryRowContext(ctx, `
				SELECT COALESCE(file_size,0), COALESCE(md5_hash,'')
				FROM client_potfiles WHERE client_id = $1 LIMIT 1`, clientID).Scan(&size, &md5); err != nil {
				return nil, nil
			}
			return &wsservice.FileInfo{
				Name: trimmed, MD5Hash: md5, Size: size,
				FileType: "client_potfile", Category: clientID,
			}, nil
		}

		var size int64
		var md5 string
		if err := r.db.QueryRowContext(ctx, `
			SELECT COALESCE(file_size,0), COALESCE(md5_hash,'')
			FROM client_wordlists
			WHERE client_id = $1 AND split_part(file_path, '/', -1) = $2 LIMIT 1`,
			clientID, filename).Scan(&size, &md5); err != nil {
			return nil, nil
		}
		return &wsservice.FileInfo{
			Name: trimmed, MD5Hash: md5, Size: size,
			FileType: "client_wordlist", Category: clientID,
		}, nil
	}

	// wordlists/association/{hashlistID}_{filename}
	if strings.HasPrefix(trimmed, "association/") {
		var size int64
		var md5 string
		if err := r.db.QueryRowContext(ctx, `
			SELECT COALESCE(file_size,0), COALESCE(md5_hash,'')
			FROM association_wordlists
			WHERE $1 LIKE '%' || split_part(file_path, '/', -1) LIMIT 1`, trimmed).Scan(&size, &md5); err != nil {
			return nil, nil
		}
		return &wsservice.FileInfo{Name: trimmed, MD5Hash: md5, Size: size, FileType: "wordlist"}, nil
	}

	var id int
	var md5 string
	var size int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(md5_hash,''), COALESCE(file_size,0)
		FROM wordlists WHERE file_name = $1 AND verification_status = 'verified' LIMIT 1`, trimmed).
		Scan(&id, &md5, &size); err != nil {
		debug.Warning("fileset: wordlist %q not resolvable, agent will fetch on demand: %v", ref, err)
		return nil, nil
	}
	return &wsservice.FileInfo{Name: trimmed, MD5Hash: md5, Size: size, FileType: "wordlist", ID: id}, nil
}

// DiskSizingOptions tunes RequiredDiskGB.
type DiskSizingOptions struct {
	// BinaryExtractionMultiplier accounts for the archive and its extracted
	// tree coexisting: ExtractBinary7z does not delete the archive.
	BinaryExtractionMultiplier float64
	// LoopbackRounds reserves room for the delta wordlists materialized per
	// loopback round, which do not exist when the parent job launches.
	LoopbackRounds int
	// SafetyFactor multiplies the total.
	SafetyFactor float64
	// MinFreeBytes mirrors the agent's own pre-flight floor
	// (minTaskFreeDiskBytes, 512 MiB) plus hashcat session/restore scratch.
	MinFreeBytes int64
}

// DefaultDiskSizing returns conservative defaults. Over-sizing costs cents;
// under-sizing a Vast.ai instance is unrecoverable, because disk is immutable
// after creation and AGENT_DISK_FULL would retry-loop until the TTL expires.
func DefaultDiskSizing() DiskSizingOptions {
	return DiskSizingOptions{
		BinaryExtractionMultiplier: 3.0,
		LoopbackRounds:             0,
		SafetyFactor:               1.25,
		MinFreeBytes:               2 << 30, // 2 GiB
	}
}

// RequiredDiskGB converts a file set into a disk size in GB, rounded up.
func (s *JobFileSet) RequiredDiskGB(opts DiskSizingOptions) int {
	var total int64
	for _, f := range s.Files {
		if f.FileType == "binary" {
			total += int64(float64(f.Size) * opts.BinaryExtractionMultiplier)
			continue
		}
		total += f.Size
		// Each loopback round materializes a fresh delta wordlist.
		if opts.LoopbackRounds > 0 && f.FileType == "wordlist" {
			total += int64(float64(f.Size)*0.05) * int64(opts.LoopbackRounds)
		}
	}
	total = int64(float64(total) * opts.SafetyFactor)
	total += opts.MinFreeBytes

	const gib = 1 << 30
	gb := int((total + gib - 1) / gib)
	if gb < 10 {
		gb = 10 // no provider is meaningfully cheaper below this
	}
	return gb
}
