const Bar = ({ width = '100%' }) => <span className="usage-skeleton-bar" style={{ width }} />;

export default function UsageLoading({ source }) {
  const local = source === 'local';
  return <>
    <section className="usage-summary usage-skeleton" aria-label="正在加载用量汇总" aria-hidden="true">
      {(local ? ['会话数', 'Token', '估算费用', '缓存读取', '活跃时长', '项目'] : ['请求数', 'Token', '记录费用', '缓存读取', '平均响应', '凭证分组']).map(label => <div className="usage-summary-cell" key={label}>
        <div className="usage-summary-label">{label}</div><div className="usage-summary-value"><Bar width="45%" /></div><div className="usage-summary-detail"><Bar width="72%" /></div>
      </div>)}
    </section>
    <section className="usage-surface usage-skeleton" aria-hidden="true"><div className="usage-section-heading"><h2>每日趋势</h2></div><div className="usage-skeleton-chart">{[45, 65, 36, 72, 50, 90, 58, 76, 40, 64, 85, 52].map((height, i) => <i key={i} style={{ height: `${height}%` }} />)}</div></section>
    <div className="usage-rankings usage-skeleton" aria-hidden="true">{[local ? '项目' : '凭证分组', '模型'].map(label => <section className="usage-surface" key={label}><div className="usage-section-heading"><h2>{label}</h2></div><div className="usage-skeleton-list">{[70, 90, 60].map((width, i) => <Bar key={i} width={`${width}%`} />)}</div></section>)}</div>
    <section className="usage-surface usage-skeleton" aria-hidden="true"><div className="usage-section-heading"><h2>活动分布</h2></div><div className="usage-skeleton-calendar"><Bar /><Bar /><Bar /><Bar /></div></section>
    <section className="usage-surface usage-skeleton" aria-hidden="true"><div className="usage-section-heading"><h2>{local ? '会话明细' : '调用明细'}</h2></div><div className="usage-skeleton-list"><Bar width="85%" /><Bar width="75%" /><Bar width="90%" /></div></section>
  </>;
}
