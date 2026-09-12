package ui

import "fmt"

type ftpTransferJob struct {
	filename   string
	localPath  string
	remotePath string
	isUpload   bool
}

type ftpTransferQueue struct {
	jobs []ftpTransferJob
	idx  int
}

func (q *ftpTransferQueue) empty() bool {
	return len(q.jobs) == 0
}

func (q *ftpTransferQueue) current() ftpTransferJob {
	if q.empty() || q.idx < 0 || q.idx >= len(q.jobs) {
		return ftpTransferJob{}
	}
	return q.jobs[q.idx]
}

func (q *ftpTransferQueue) startOrEnqueue(job ftpTransferJob) (ftpTransferJob, bool) {
	if q.empty() {
		q.jobs = []ftpTransferJob{job}
		q.idx = 0
		return job, true
	}
	q.jobs = append(q.jobs, job)
	return ftpTransferJob{}, false
}

func (q *ftpTransferQueue) finishCurrent() (ftpTransferJob, bool) {
	if q.empty() {
		return ftpTransferJob{}, false
	}
	if q.idx+1 < len(q.jobs) {
		q.idx++
		return q.jobs[q.idx], true
	}
	q.clear()
	return ftpTransferJob{}, false
}

func (q *ftpTransferQueue) clear() {
	q.jobs = nil
	q.idx = 0
}

func (q *ftpTransferQueue) position() (cur, total int) {
	if q.empty() {
		return 0, 0
	}
	return q.idx + 1, len(q.jobs)
}

func staleFTPProgress(transferring bool, currentGen, msgGen int) bool {
	return !transferring || currentGen != msgGen
}

func formatFTPProgress(job ftpTransferJob, done, total int64, cur, count int) string {
	action := "Downloading"
	if job.isUpload {
		action = "Uploading"
	}
	var body string
	if total > 0 {
		pct := done * 100 / total
		body = fmt.Sprintf("%s %s: %s / %s (%d%%)",
			action, job.filename, formatSize(done), formatSize(total), pct)
	} else {
		body = fmt.Sprintf("%s %s: %s", action, job.filename, formatSize(done))
	}
	if count > 1 {
		body = fmt.Sprintf("%s  [%d/%d]", body, cur, count)
	}
	return body
}
