local grafana = import '/home/nickcabatoff/src/github.com/grafana/grafonnet-lib/grafonnet/grafana.libsonnet';
{
  dashboard:: grafana.dashboard,
  instre:: 'instance=~"$instance"',
  instgnre:: 'instance=~"$instance", groupname=~"$groupname"',
  ssheight:: 3,
  sswidth:: 4,
  gheight:: 8,
  gwidth:: 12,

  link(title, tags)::
    grafana.link.dashboards(
      title,
      tags,
      includeVars=true,
      keepTime=true,
    ),

  dstmpl()::
    grafana.template.datasource(
      'PROMETHEUS_DS',
      'prometheus',
      'Prometheus',
      hide='label',
    ),

  ivtmpl()::
    grafana.template.interval(
      'interval',
      'auto,10s,1m,2m,5m,10m,30m,1h',
      'auto',
    ),

  tmpl(name, label_expr)::
    grafana.template.new(
      name,
      '$PROMETHEUS_DS',
      'label_values(%s, %s)' % [label_expr, name],
      refresh='time',
      includeAll=true,
      multi=true,
    ),

  simpless(title, expr)::
    grafana.singlestat.new(
      title,
      datasource='$PROMETHEUS_DS',
      valueName='current',
    )
    .addTarget(
      grafana.prometheus.target(expr),
    ),

  defgraph(title, format='short')::
    grafana.graphPanel.new(
      title,
      min=0,
      legend_values=true,
      legend_min=true,
      legend_max=true,
      legend_current=true,
      legend_total=false,
      legend_avg=true,
      legend_alignAsTable=true,
      format=format,
    ),

  gntarget(expr)::
    grafana.prometheus.target(
      "sum by (groupname) (%s)" % expr,
      datasource='$PROMETHEUS_DS',
      legendFormat='{{ groupname }}'
    ),

  encurl(str)::
    "{{ %s }}" % str,

  multitarget(labels, expr)::
    grafana.prometheus.target(
      "sum by (%s) (%s)" % [std.join(',', labels), expr],
      datasource='$PROMETHEUS_DS',
      legendFormat=std.join('', std.map($.encurl, labels)),
    ),
}