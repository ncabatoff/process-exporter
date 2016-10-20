# process-exporter
Prometheus exporter that mines /proc to report on selected processes.

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

CPU usage comes from /proc/[pid]/stat fields utime (user time) and stime (system
time.)  It has been translated into fractional seconds of CPU consumed during
the polling interval.

Bytes read and written come from /proc/[pid]/io in recent enough kernels.
These correspond to the fields read_bytes and write_bytes respectively.

An example Grafana dashboard to view the metrics is available at https://grafana.net/dashboards/249

## History

An earlier version of this exporter had options to enable auto-discovery of
which processes were consuming resources.  This functionality has been removed.
These options were based on a percentage of resource usage, e.g. if an
untracked process consumed X% of CPU during a scrape, start tracking processes
with that name.  However during any given scrape it's likely that most
processes are idle, so we could add a process that consumes minimal resources
but which happened to be active during the interval preceding the current
scrape.  Over time this means that a great many processes wind up being
scraped, which becomes unmanageable to visualize.  This could be mitigated by
looking at resource usage over longer intervals, but ultimately I didn't feel
this feature was important enough to invest more time in at this point.  It may
re-appear at some point in the future, but no promises.

Another lost feature: the "other" group was used to count usage by non-tracked
procs.  This was useful to get an idea of what wasn't being monitored.  But it
comes at a high cost: if you know what processes you care about, you're wasting
a lot of CPU to compute the usage of everything else that you don't care about.
The new approach is to minimize resources expended on non-tracked processes and
to require the user to whitelist the processes to track.  
