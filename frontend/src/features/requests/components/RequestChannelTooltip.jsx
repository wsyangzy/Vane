export default function RequestChannelTooltip({ nodes, channelLabel }) {
  if (!nodes.length) return <span>{channelLabel === '未发送' || channelLabel === '待调度' ? `${channelLabel}，尚无实际调用路径` : '此记录未保存渠道、分组和端点信息'}</span>;
  return <div className="max-w-full text-left font-normal" aria-label="实际调用链路">
    <ol className={`relative space-y-1 pl-3 ${nodes.length > 1 ? "before:absolute before:bottom-3 before:left-[3px] before:top-3 before:w-px before:bg-slate-200" : ''}`}>
      {nodes.map((node, index) => {
        const first = index === 0;
        const last = index === nodes.length - 1;
        const role = nodes.length === 1 ? '直达' : first ? '起始' : last ? '最终' : '中转';
        const tone = nodes.length === 1 ? 'bg-indigo-500' : first ? 'bg-blue-500' : last ? 'bg-emerald-500' : 'bg-slate-400';
        return <li key={index} className="relative flex min-w-0 items-center gap-1.5 whitespace-nowrap rounded border border-slate-200 bg-slate-50/70 px-2 py-1 text-[11px]" aria-label={`${role}：${node.channel || '渠道未记录'} / ${node.credentialGroup || '分组未记录'} / ${node.endpoint || '端点未记录'}`}>
          <span aria-hidden="true" className={`absolute -left-[13px] top-1/2 h-2 w-2 -translate-y-1/2 rounded-full ring-2 ring-white ${tone}`} />
          <span className="shrink-0 text-[10px] text-slate-400">{index + 1}·{role}</span>
          <span className="min-w-0 truncate font-medium text-purple-700">{node.channel || '渠道未记录'}</span>
          <span className="shrink-0 text-slate-300">/</span><span className="min-w-0 truncate text-violet-700">{node.credentialGroup || '分组未记录'}</span>
          <span className="shrink-0 text-slate-300">/</span><span className="min-w-0 truncate text-slate-700">{node.endpoint || '端点未记录'}</span>
          {node.attempts > 1 && <span className="shrink-0 text-[10px] text-slate-400">×{node.attempts}</span>}
        </li>;
      })}
    </ol>
  </div>;
}
