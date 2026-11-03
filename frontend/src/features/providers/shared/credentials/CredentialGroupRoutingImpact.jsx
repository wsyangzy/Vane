export default function CredentialGroupRoutingImpact({ impact }) {
  if (impact.loading) return <p className="mt-3 text-xs text-slate-500" role="status">正在检查调度方案引用…</p>;
  if (impact.error) return <p className="mt-3 text-xs text-rose-600 wrap-anywhere" role="alert">{impact.error}。请取消后重试，确认引用情况后再删除。</p>;
  if (!impact.profiles.length) return null;
  return <div className="mt-3 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900" role="status">
    <p className="font-medium">该分组被 {impact.profiles.length} 个调度方案引用，暂不可删除</p>
    <ul className="mt-2 max-h-36 space-y-1 overflow-y-auto text-xs">{impact.profiles.map((profile) => <li key={`${profile.provider}/${profile.id}`} className="wrap-anywhere">{profile.provider === 'claude' ? 'Claude' : 'Codex'} · {profile.name}</li>)}</ul>
    <p className="mt-2 text-xs leading-5">请先取消该分组的参与，或移除规则与识别器中的引用。停用方案中已勾选的分组也计入；选择“全部分组”同样包含该分组。分组名称等配置仍可正常编辑。</p>
    {impact.profiles.some((profile) => profile.classifierReference) && <p className="mt-1 text-xs leading-5">该分组也被意图识别器使用，请先更换识别凭证。</p>}
  </div>;
}
