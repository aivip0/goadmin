package media

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
)

const maintenanceLockKey int64 = 0x676f61646d726563
const reconcileByteBudget int64 = 250 << 20

type ReconcileReport struct {
	Processed       int `json:"processed"`
	Failed          int `json:"failed"`
	StagingCleaned  int `json:"staging_cleaned"`
	OrphansIsolated int `json:"orphans_isolated"`
	ObjectsScanned  int `json:"objects_scanned"`
}

// Reconcile runs at most 30s, touches at most 100 due rows and 100 ready staging rows.
// A separate advisory lock ensures concurrent maintenance commands cannot race.
func Reconcile(ctx context.Context, r *Repository, s *LocalStorage, actor string) (ReconcileReport, error) {
	var result ReconcileReport
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	var locked bool
	if err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, maintenanceLockKey).Scan(&locked); err != nil {
		return result, err
	}
	if !locked {
		return result, ErrConflict
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, maintenanceLockKey)
	if err = ValidateMaintenanceRoot(ctx, r, s); err != nil {
		return result, err
	}
	if !uuidName.MatchString(actor) {
		return result, ErrInvalid
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+fileFields+`,status,reconcile_attempts FROM media_files WHERE status IN ('pending','deleting','delete_failed') AND next_reconcile_at<=clock_timestamp() AND (reconcile_claim_until IS NULL OR reconcile_claim_until<clock_timestamp()) ORDER BY next_reconcile_at,id LIMIT 100`)
	if err != nil {
		return result, err
	}
	return reconcileRows(ctx, r, s, actor, rows)
}

func ValidateMaintenanceRoot(ctx context.Context, r *Repository, s *LocalStorage) error {
	rootID, err := r.RootID(ctx)
	if err != nil {
		return ErrNotReady
	}
	info, err := s.root.Lstat(".goadmin-media-root")
	if err != nil || !info.Mode().IsRegular() {
		return ErrNotReady
	}
	f, err := s.root.Open(".goadmin-media-root")
	if err != nil {
		return ErrNotReady
	}
	marker, e := io.ReadAll(io.LimitReader(f, 65))
	f.Close()
	if e != nil || string(marker) != rootID+"\n" {
		return ErrNotReady
	}
	if _, err = r.generation(ctx); err != nil {
		return err
	}
	return nil
}

func reconcileRows(ctx context.Context, r *Repository, s *LocalStorage, actor string, rows *sql.Rows) (ReconcileReport, error) {
	var result ReconcileReport
	var err error
	type due struct {
		File
		status   string
		attempts int
	}
	items := []due{}
	for rows.Next() {
		var row due
		var hash []byte
		err = rows.Scan(&row.ID, &row.OperationID, &row.Key, &row.OriginalName, &row.MIME, &row.Size, &hash, &row.Width, &row.Height, &row.CreatedAt, &row.status, &row.attempts)
		if err != nil {
			rows.Close()
			return result, err
		}
		copy(row.Digest[:], hash)
		items = append(items, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	var diskBytes int64
	for _, item := range items {
		if diskBytes+2*item.Size > reconcileByteBudget {
			break
		}
		diskBytes += 2 * item.Size
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		claim := uuid()
		res, e := r.db.ExecContext(ctx, `UPDATE media_files SET reconcile_claim_id=$2,reconcile_claim_until=clock_timestamp()+interval '45 seconds' WHERE id=$1 AND status=$3 AND (reconcile_claim_until IS NULL OR reconcile_claim_until<clock_timestamp())`, item.ID, claim, item.status)
		if e != nil {
			return result, e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			continue
		}
		result.Processed++
		var operationErr error
		switch item.status {
		case "pending":
			// Published object may be sound even when the request crashed before ready commit.
			operationErr = s.VerifyObject(ctx, item.Key, item.Size, item.Digest)
			if operationErr == nil {
				operationErr = r.RecoveredReady(ctx, actor, item.ID, claim)
				if operationErr == nil {
					_ = s.RemoveStage(item.OperationID)
					_ = s.RemoveIncoming(item.OperationID)
				}
			} else if errors.Is(operationErr, os.ErrNotExist) {
				// An incomplete Put can be retried from the durable staging copy.
				if incoming, e := s.InspectIncoming(item.OperationID); e == nil && incoming {
					operationErr = s.PublishIncoming(ctx, item.OperationID, item.Key, item.Size, item.Digest)
					if errors.Is(operationErr, ErrCorrupt) {
						if e = s.RemoveIncoming(item.OperationID); e != nil {
							operationErr = e
						}
					}
				}
				if errors.Is(operationErr, os.ErrNotExist) || errors.Is(operationErr, ErrCorrupt) {
					if stage, e := s.InspectStage(item.OperationID); e == nil && stage {
						operationErr = s.Put(ctx, item.OperationID, item.Key, item.Size, item.Digest)
					}
				}
				if operationErr == nil {
					operationErr = r.RecoveredReady(ctx, actor, item.ID, claim)
					if operationErr == nil {
						_ = s.RemoveStage(item.OperationID)
					}
				}
			}
		case "deleting", "delete_failed":
			operationErr = s.Delete(ctx, item.Key)
			if operationErr == nil {
				operationErr = r.RecoveredDeleted(ctx, actor, item.ID, claim)
			}
		}
		if operationErr != nil {
			result.Failed++
			attempts := item.attempts + 1
			var next any
			if attempts < 5 {
				next = time.Now().UTC().Add(retryAt(attempts))
			}
			newStatus := item.status
			if item.status == "deleting" {
				newStatus = "delete_failed"
			}
			if item.status == "pending" && attempts >= 5 {
				newStatus = "failed"
			}
			_, e = r.db.ExecContext(ctx, `UPDATE media_files SET status=$3,reconcile_attempts=$4,next_reconcile_at=$5,last_error_code='reconcile_failed',reconcile_claim_id=NULL,reconcile_claim_until=NULL,updated_at=clock_timestamp() WHERE id=$1 AND reconcile_claim_id=$2`, item.ID, claim, newStatus, attempts, next)
			if e != nil {
				return result, e
			}
		}
	}
	// Independently scan a bounded portion of ready rows for leftover staging; never delete objects.
	var cursor string
	if err = r.db.QueryRowContext(ctx, `SELECT COALESCE(local_scan_cursors->>'ready_id','') FROM media_storage_binding WHERE singleton=true`).Scan(&cursor); err != nil {
		return result, err
	}
	ready, err := r.db.QueryContext(ctx, `SELECT id::text,upload_operation_id::text,storage_key,byte_size,sha256 FROM media_files WHERE status='ready' AND ($1='' OR id::text>$1) ORDER BY id LIMIT 100`, cursor)
	if err != nil {
		return result, err
	}
	type remainder struct {
		id      string
		op, key string
		size    int64
		hash    [32]byte
	}
	left := []remainder{}
	for ready.Next() {
		var v remainder
		var hash []byte
		if err = ready.Scan(&v.id, &v.op, &v.key, &v.size, &hash); err != nil {
			ready.Close()
			return result, err
		}
		copy(v.hash[:], hash)
		left = append(left, v)
	}
	err = ready.Err()
	ready.Close()
	if err != nil {
		return result, err
	}
	for _, v := range left {
		if diskBytes+v.size > reconcileByteBudget {
			break
		}
		diskBytes += v.size
		cursor = v.id
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		exists, e := s.InspectStage(v.op)
		if e != nil {
			return result, e
		}
		if !exists {
			continue
		}
		var dbKey, status string
		if e = r.db.QueryRowContext(ctx, `SELECT storage_key,status FROM media_files WHERE upload_operation_id=$1`, v.op).Scan(&dbKey, &status); e != nil {
			return result, e
		}
		if dbKey != v.key || status != "ready" {
			continue
		}
		if e = s.VerifyObject(ctx, v.key, v.size, v.hash); e != nil {
			continue
		}
		if e = s.RemoveStage(v.op); e == nil {
			result.StagingCleaned++
		}
	}
	if len(left) < 100 && (len(left) == 0 || cursor == left[len(left)-1].id) {
		cursor = ""
	}
	_, err = r.db.ExecContext(ctx, `UPDATE media_storage_binding SET local_scan_cursors=jsonb_set(local_scan_cursors,'{ready_id}',to_jsonb($1::text)) WHERE singleton=true`, cursor)
	if err != nil {
		return result, err
	}
	// Unknown temporary files are quarantined only after a 24h grace period and two DB checks.
	for _, kind := range []string{"staging", "incoming"} {
		var scanCursor string
		if e := r.db.QueryRowContext(ctx, `SELECT COALESCE(local_scan_cursors->>$1,'') FROM media_storage_binding WHERE singleton=true`, kind).Scan(&scanCursor); e != nil {
			return result, e
		}
		candidates, e := s.ScanTemp(kind, 50, scanCursor)
		if e != nil {
			return result, e
		}
		for _, candidate := range candidates {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			if time.Since(candidate.Modified) < 24*time.Hour {
				continue
			}
			var exists bool
			for range 2 {
				if e = r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM media_files WHERE upload_operation_id=$1)`, candidate.OperationID).Scan(&exists); e != nil {
					return result, e
				}
				if exists {
					break
				}
			}
			if exists {
				continue
			}
			// A maintenance lock excludes other reconcilers; a second DB check narrows upload races.
			if e = s.QuarantineTemp(kind, candidate.OperationID); e != nil {
				result.Failed++
				continue
			}
			result.OrphansIsolated++
		}
		if len(candidates) > 0 {
			scanCursor = candidates[len(candidates)-1].OperationID
		} else {
			scanCursor = ""
		}
		if _, e = r.db.ExecContext(ctx, `UPDATE media_storage_binding SET local_scan_cursors=jsonb_set(local_scan_cursors,ARRAY[$1],to_jsonb($2::text)) WHERE singleton=true`, kind, scanCursor); e != nil {
			return result, e
		}
	}
	// One of 256 opaque-key shards per invocation. The persisted cursor makes
	// progress across invocations; an oversized shard fails closed for review.
	var shard, objectCursor string
	if err = r.db.QueryRowContext(ctx, `SELECT COALESCE(local_scan_cursors->>'object_shard','00'),COALESCE(local_scan_cursors->>'object_key','') FROM media_storage_binding WHERE singleton=true`).Scan(&shard, &objectCursor); err != nil {
		return result, err
	}
	if !prefixName.MatchString(shard) {
		return result, ErrNotReady
	}
	objects, err := s.ScanObjects(shard, objectCursor, 50)
	if err != nil {
		return result, err
	}
	for _, object := range objects {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		objectCursor = object.Key
		result.ObjectsScanned++
		if time.Since(object.Modified) < 24*time.Hour {
			continue
		}
		// The core lock in QuarantineOrphan prevents an upload from claiming
		// this key after the absence check. Never remove an uncertain object.
		isolated, e := r.QuarantineOrphan(ctx, s, object.Key)
		if errors.Is(e, os.ErrNotExist) || errors.Is(e, ErrConflict) {
			continue
		}
		if e != nil {
			return result, e
		}
		if isolated {
			result.OrphansIsolated++
		}
	}
	if len(objects) < 50 {
		next, _ := strconv.ParseUint(shard, 16, 8)
		shard = fmt.Sprintf("%02x", (next+1)%256)
		objectCursor = ""
	}
	_, err = r.db.ExecContext(ctx, `UPDATE media_storage_binding SET local_scan_cursors=jsonb_set(jsonb_set(local_scan_cursors,'{object_shard}',to_jsonb($1::text)),'{object_key}',to_jsonb($2::text)) WHERE singleton=true`, shard, objectCursor)
	if err != nil {
		return result, err
	}
	return result, nil
}
