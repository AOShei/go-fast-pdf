# Sourced by gen.sh and fetch.sh. Not run by itself.
#
# contained <seconds> <megabytes> <command...>
#
# Runs a command so that it cannot outlive a time limit or fill the disk:
#
#   - The command runs in a process group of its own, and the whole group
#     is killed at the time limit and again when the command returns. A
#     program that works through a child (LibreOffice's launcher does)
#     would otherwise leave the child running.
#   - No file it writes can exceed the size limit, and it cannot use more
#     processor time than its time limit. The kernel enforces both, so
#     they hold even if this script is killed.
#
# The command's output and exit status pass through unchanged. A command
# that was stopped returns a status above 128.
contained() {
	local seconds=$1 megabytes=$2 status=0 pid watchdog
	shift 2
	# With job control on, each background job gets its own process group,
	# whose number is the job's process id.
	set -m
	(
		# bash counts the file-size limit in blocks of 1024 bytes; a shell
		# in POSIX mode counts 512, which gives half the size.
		# A limit can only be lowered. If a tighter one is already in
		# force, that one stays, which is fine.
		ulimit -f $((megabytes * 1024)) 2>/dev/null || true
		ulimit -t $((seconds + 10)) 2>/dev/null || true
		exec "$@"
	) &
	pid=$!
	(
		sleep "$seconds"
		kill -KILL -- "-$pid" 2>/dev/null
	) >/dev/null 2>&1 &
	watchdog=$!
	set +m
	wait "$pid" 2>/dev/null || status=$?
	kill -KILL -- "-$watchdog" 2>/dev/null || true
	wait "$watchdog" 2>/dev/null || true
	# Anything the command started and left behind.
	kill -KILL -- "-$pid" 2>/dev/null || true
	return "$status"
}
