# process-exporter
Prometheus exporter that mines /proc to report on selected processes 

The premise for this exporter is that sometimes you have apps that are
impractical to instrument directly, either because you don't control the code
or they're written in a language that isn't easy to instrument with Prometheus.
A fair bit of information can be gleaned from /proc, especially for
long-running programs.

For most systems it won't be beneficial to create metrics for every process by
name: there are just too many of them and most don't do enough to merit it.
Various command-line options are provided to control how processes are grouped
and the groups are named.  Run "process-exporter -man" to see a help page
giving details.

Metrics available currently include CPU usage, bytes written and read, and
number of processes in each group.  They come with plenty of caveats, see
either the Linux kernel documentation or man 5 proc.

CPU usage comes from /proc/[pid]stat fields utime (user time) and stime (system
time.)  It has been translated into fractional seconds of CPU consumed during
the polling interval.

Bytes read and written come from /proc/[pid]/io in recent enough kernels.
These correspond to the fields read_bytes and write_bytes respectively.

