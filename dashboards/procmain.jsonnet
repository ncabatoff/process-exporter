local grproc = import 'grproc.libsonnet';

grproc.dashboard.new(
  'procexp',
  schemaVersion=16,
  tags=['os', 'procs'],
  graphTooltip='shared_crosshair',
)
.addLink(grproc.link("procs", ['procs']))
.addTemplate(grproc.dstmpl())
.addTemplate(grproc.ivtmpl())
.addTemplate(grproc.tmpl('instance', 'namedprocess_namegroup_num_procs'))
.addTemplate(grproc.tmpl('groupname', 'namedprocess_namegroup_num_procs{%s}' % grproc.instre))
.addPanel(
  grproc.simpless('exporter up', 'up{job="process-exporter"}'),
  gridPos={x: 0, y: 0, w: grproc.sswidth, h: grproc.ssheight},
)
.addPanel(
  grproc.simpless('scrape time', 'sum(scrape_duration_seconds{%s, job="process-exporter"})' % grproc.instre)
  +{format: 's'},
  gridPos={x: 1*grproc.sswidth, y: 0, w: grproc.sswidth, h: grproc.ssheight},
)
.addPanel(
  grproc.simpless('samples', 'sum(scrape_samples_scraped{%s, job="process-exporter"})' % grproc.instre),
  gridPos={x: 2*grproc.sswidth, y: 0, w: grproc.sswidth, h: grproc.ssheight},
)
.addPanel(
  grproc.simpless('exporter errs', 'sum(namedprocess_scrape_errors{%s})' % grproc.instre),
  gridPos={x: 3*grproc.sswidth, y: 0, w: grproc.sswidth, h: grproc.ssheight},
)
.addPanel(
  grproc.simpless('threads running', 'sum(namedprocess_namegroup_states{%s, state="Running"})' % grproc.instre)
  +{sparkline: {show: true}},
  gridPos={x: 4*grproc.sswidth, y: 0, w: grproc.sswidth, h: grproc.ssheight},
)
.addPanel(
  grproc.simpless('threads blocked', 'sum(namedprocess_namegroup_states{%s, state="Waiting"})' % grproc.instre)
  +{sparkline: {show: true}},
  gridPos={x: 5*grproc.sswidth, y: 0, w: grproc.sswidth, h: grproc.ssheight},
)
.addPanel(
  grproc.defgraph('Num procs', format='short')
  .addTarget(
    grproc.gntarget('namedprocess_namegroup_num_procs{%s}' % grproc.instgnre)
  ),
  gridPos={x: 0, y: 1*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('Restarts')
  .addTarget(
    grproc.gntarget(|||
      changes(namedprocess_namegroup_oldest_start_time_seconds{%s}[$interval])
    ||| % grproc.instgnre)
  ),
  gridPos={x: 1*grproc.gwidth, y: 1*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('CPU', format='s')
  .addTarget(
    grproc.gntarget(|||
      rate(namedprocess_namegroup_cpu_user_seconds_total{%s}[$interval]) +
      rate(namedprocess_namegroup_cpu_system_seconds_total{%s}[$interval])
    ||| % [grproc.instgnre, grproc.instgnre])
  ),
  gridPos={x: 0, y: 2*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('I/O', format='bytes')
  .addTarget(
    grproc.gntarget(|||
      rate(namedprocess_namegroup_read_bytes_total{%s}[$interval]) +
      rate(namedprocess_namegroup_write_bytes_total{%s}[$interval])
    ||| % [grproc.instgnre, grproc.instgnre])
  ),
  gridPos={x: 1*grproc.gwidth, y: 2*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('Resident Memory', format='bytes')
  .addTarget(
    grproc.gntarget('namedprocess_namegroup_memory_bytes{%s, memtype="resident"}' % grproc.instgnre)
  ),
  gridPos={x: 0, y: 3*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('Worst FD Ratio')
  .addTarget(
    grproc.gntarget('namedprocess_namegroup_worst_fd_ratio{%s}' % grproc.instgnre)
  ),
  gridPos={x: 1*grproc.gwidth, y: 3*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)