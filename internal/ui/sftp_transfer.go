package ui

import "fmt"

type sftpTransferJob struct {
	filename   string
	localPath  string
	remotePath string
	isUpload   bool
}

type sftpTransferQueue struct {
	jobs []sftpTransferJob
	idx  int
}

func (q *sftpTransferQueue) empty() bool {
	return len(q.jobs) == 0
}

func (q *sftpTransferQueue) current() sftpTransferJob {
	if q.empty() || q.idx < 0 || q.idx >= len(q.jobs) {
		return sftpTransferJob{}
	}
	return q.jobs[q.idx]
}

func (q *sftpTransferQueue) position() (cur, total int) {
	if q.empty() {
		return 0, 0
	}
	return q.idx + 1, len(q.jobs)
}

// startOrEnqueue starts job immediately when idle, otherwise queues it
// behind the in-flight transfer. started is true only when this job
// should begin now.
func (q *sftpTransferQueue) startOrEnqueue(job sftpTransferJob) (sftpTransferJob, bool) {
	if q.empty() {
		q.jobs = []sftpTransferJob{job}
		q.idx = 0
		return job, true
	}
	q.jobs = append(q.jobs, job)
	return sftpTransferJob{}, false
}

func (q *sftpTransferQueue) finishCurrent() (sftpTransferJob, bool) {
	if q.empty() {
		return sftpTransferJob{}, false
	}
	if q.idx+1 < len(q.jobs) {
		q.idx++
		return q.jobs[q.idx], true
	}
	q.clear()
	return sftpTransferJob{}, false
}

func (q *sftpTransferQueue) clear() {
	q.jobs = nil
	q.idx = 0
}

func staleSFTPProgress(transferring bool, currentGen, msgGen int) bool {
	return !transferring || currentGen != msgGen
}

func formatSFTPProgress(job sftpTransferJob, done, total int64, cur, count int) string {
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
