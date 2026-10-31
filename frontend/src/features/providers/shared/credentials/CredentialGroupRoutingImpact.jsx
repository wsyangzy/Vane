export default function CredentialGroupRoutingImpact({ impact }) {
  if (impact.loading) return <p className="mt-3 text-xs text-slate-500" role="status">正在检查调度方案引用…</p>;
  if (impact.error) return <p className="mt-3 text-xs text-rose-600 wrap-anywhere" role="alert">{impact.error}。请取消后重试，确认引用情况后再删除。</p>;
  if (!impact.profiles.length) return null;
  return <div className="mt-3 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900" role="status">
    <p className="font-medium">该分组关联 {impact.profiles.length} 个调度方案</p>
    <ul className="mt-2 max-h-36 space-y-1 overflow-y-auto text-xs">{impact.profiles.map((profile) => <li key={`${profile.provider}/${profile.id}`} className="wrap-anywhere">{profile.provider === 'claude' ? 'Claude' : 'Codex'} · {profile.name}</li>)}</ul>
    <p className="mt-2 text-xs leading-5">确认删除后，将同步移除方案中的该分组引用，并停用因此失去全部目标的模型映射。选择全部分组的渠道会保留，继续使用其余分组。</p>
    {impact.profiles.some((profile) => profile.classifierReference) && <p className="mt-1 text-xs leading-5">使用该分组的意图识别器将停用，请重新选择识别凭证后启用。</p>}
    {impact.profiles.some((profile) => profile.losesLastMember) && <p className="mt-1 text-xs font-medium leading-5">部分方案将没有参与分组，需要补充可用分组后才能继续调度。</p>}
  </div>;
}
