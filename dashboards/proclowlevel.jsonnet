local grproc = import 'grproc.libsonnet';

grproc.dashboard.new(
  'proclow',
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
  grproc.defgraph('Num threads', format='short')
  .addTarget(
    grproc.gntarget('namedprocess_namegroup_num_threads{%s}' % grproc.instgnre)
  ),
  gridPos={x: 0, y: 1*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('Thread waiting on channels', format='short')
  .addTarget(
    grproc.gntarget('namedprocess_namegroup_threads_wchan{%s}' % grproc.instgnre)
  ),
  gridPos={x: 1*grproc.gwidth, y: 1*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('Running Threads', format='short')
  .addTarget(
    grproc.gntarget('namedprocess_namegroup_state{%s, state="Running"}' % grproc.instgnre)
  ),
  gridPos={x: 0, y: 2*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('Waiting Threads', format='short')
  .addTarget(
    grproc.multitarget(['groupname', 'wchan'], 'namedprocess_namegroup_state{%s}' % grproc.instgnre)
  ),
  gridPos={x: 1*grproc.gwidth, y: 2*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('Zombie Procs', format='short')
  .addTarget(
    grproc.gntarget('namedprocess_namegroup_state{%s, state="Zombie"}' % grproc.instgnre)
  ),
  gridPos={x: 0, y: 3*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('Other Procs', format='short')
  .addTarget(
    grproc.gntarget('namedprocess_namegroup_state{%s, state="Other"}' % grproc.instgnre)
  ),
  gridPos={x: 1*grproc.gwidth, y: 3*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('Voluntary Context Switches', format='short')
  .addTarget(
    grproc.gntarget('rate(namedprocess_namegroup_context_switches_total{%s, ctxswitchtype="voluntary"}[$interval])' % grproc.instgnre)
  ),
  gridPos={x: 0, y: 4*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)
.addPanel(
  grproc.defgraph('Nonvoluntary Context Switches', format='short')
  .addTarget(
    grproc.gntarget('rate(namedprocess_namegroup_context_switches_total{%s, ctxswitchtype="nonvoluntary"}[$interval])' % grproc.instgnre)
  ),
  gridPos={x: 1*grproc.gwidth, y: 4*grproc.ssheight, w: grproc.gwidth, h: grproc.gheight},
)