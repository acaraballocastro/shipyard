// A tiny trend line. Values are drawn against [0, max(max(values), floor)]
// so an idle service stays flat at the bottom instead of being stretched.
export function Sparkline({
  values,
  floor = 1,
  height = 28,
  label,
}: {
  values: number[];
  floor?: number;
  height?: number;
  label: string;
}) {
  const w = 120;
  if (values.length < 2) {
    return <svg className="spark empty" viewBox={`0 0 ${w} ${height}`} role="img" aria-label={`${label}: collecting`} />;
  }
  const max = Math.max(floor, ...values);
  const step = w / (values.length - 1);
  const pts = values.map((v, i) => [i * step, height - 2 - (v / max) * (height - 4)]);
  const line = pts.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  const last = pts[pts.length - 1];
  return (
    <svg className="spark" viewBox={`0 0 ${w} ${height}`} preserveAspectRatio="none" role="img" aria-label={label}>
      <polygon className="spark-area" points={`0,${height} ${line} ${w},${height}`} />
      <polyline className="spark-line" points={line} vectorEffect="non-scaling-stroke" />
      <circle className="spark-dot" cx={last[0]} cy={last[1]} r="2" />
    </svg>
  );
}
