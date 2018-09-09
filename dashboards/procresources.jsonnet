local grproc = import 'grproc.libsonnet';

grproc.dashboard.new(
  'procres',
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
  grproc.defgraph('CPU user', format='s')
  .addTarget(
    grproc.gntarget('rate(namedprocess_namegroup_cpu_user_seconds_total{%s}[$interval])' % grproc.instgnre)
  ),
  gridPos={x: 0, y: 1*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('CPU system', format='s')
  .addTarget(
    grproc.gntarget('rate(namedprocess_namegroup_cpu_system_seconds_total{%s}[$interval])' % grproc.instgnre)
  ),
  gridPos={x: 1*grproc.gwidth, y: 1*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('I/O read', format='bytes')
  .addTarget(
    grproc.gntarget('rate(namedprocess_namegroup_read_bytes_total{%s}[$interval])' % grproc.instgnre)
  ),
  gridPos={x: 0, y: 2*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('I/O write', format='bytes')
  .addTarget(
    grproc.gntarget('rate(namedprocess_namegroup_write_bytes_total{%s}[$interval])' % grproc.instgnre)
  ),
  gridPos={x: 1*grproc.gwidth, y: 2*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('Major page faults')
  .addTarget(
    grproc.gntarget('rate(namedprocess_namegroup_major_page_faults_total{%s}[$interval])' % grproc.instgnre)
  ),
  gridPos={x: 0, y: 3*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('Swapped Memory', format='bytes')
  .addTarget(
    grproc.gntarget('namedprocess_namegroup_memory_bytes{%s, memtype="swapped"}' % grproc.instgnre)
  ),
  gridPos={x: 1*grproc.gwidth, y: 3*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('Resident Memory', format='bytes')
  .addTarget(
    grproc.gntarget('namedprocess_namegroup_memory_bytes{%s, memtype="resident"}' % grproc.instgnre)
  ),
  gridPos={x: 0, y: 4*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('Virtual Memory', format='bytes')
  .addTarget(
    grproc.gntarget('namedprocess_namegroup_memory_bytes{%s, memtype="virtual"}' % grproc.instgnre)
  ),
  gridPos={x: 1*grproc.gwidth, y: 4*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)