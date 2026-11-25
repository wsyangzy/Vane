export default function RequestChannelTooltip({ nodes, channelLabel, resources = [], cooldown = false }) {
  if (!nodes.length) return <span>{cooldown ? '候选端点冷却，本次未向上游发送' : channelLabel === '未发送' || channelLabel === '待调度' ? `${channelLabel}，尚无实际调用路径` : '此记录未保存渠道、分组和端点信息'}</span>;
  return <div className="max-w-full text-left font-normal" aria-label="实际调用链路">
    {resources.length > 1 && <p className="mb-1.5 border-b border-slate-700 pb-1.5 font-medium">{resources.join(' → ')}</p>}
    <ol className={`relative space-y-1 pl-3 ${nodes.length > 1 ? 'before:absolute before:bottom-3 before:left-[3px] before:top-2 before:w-px before:bg-slate-600' : ''}`}>
      {nodes.map((node, index) => {
        const first = index === 0;
        const last = index === nodes.length - 1;
        const role = nodes.length === 1 ? '实际发送' : first ? '首次发送' : last ? '最后发送' : '中转';
        return <li key={index} className="relative grid grid-cols-[auto_minmax(0,1fr)] gap-x-2 text-[11px] leading-4">
          <span aria-hidden="true" className={`absolute -left-[11px] top-[5px] size-1.5 rounded-full ${last ? 'bg-indigo-300' : 'bg-slate-400'}`} />
          <span className="text-slate-300">{role}</span>
          <span>{[node.channel, node.credentialGroup, node.endpoint].filter(Boolean).join(' / ') || '资源未记录'}{node.attempts > 1 && <span className="ml-1 text-slate-300">×{node.attempts}</span>}</span>
        </li>;
      })}
    </ol>
  </div>;
}
