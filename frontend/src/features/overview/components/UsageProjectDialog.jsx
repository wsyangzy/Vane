import { useEffect, useId, useRef, useState } from 'react';
import { ChevronDown, ChevronLeft, ChevronRight, FolderClosed, FolderOpen, LoaderCircle, Search, X } from 'lucide-react';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { Tooltip } from '@/shared/ui/index.js';
import useUsageDashboard from '../hooks/useUsageDashboard.js';
import { count, duration, money, projectName, projectParent, sessionTitle, unrecordedProject, usageDate } from '../usageDashboard.js';
import UsageSessionRow from './UsageSessionRow.jsx';

function ProjectSessions({ project, query, selected, onSelect }) {
  const [page, setPage] = useState(1);
  const { data, loading, previous, error, refresh } = useUsageDashboard({ ...query, project: project.key || unrecordedProject, page, pageSize: 20 }, 0);
  const pages = Math.max(1, Math.ceil((data?.rowCount || 0) / 20));
  return <div className="usage-project-sessions" aria-busy={loading}>
    {loading && <span className="usage-project-status" role="status"><LoaderCircle size={13} className="animate-spin" />正在读取项目会话{previous ? ' · 保留上次结果' : ''}</span>}
    {error && <div className="usage-error" role="alert"><span>{error}</span><button type="button" onClick={refresh} disabled={loading}>重试</button></div>}
    {(data?.rows || []).map(row => <UsageSessionRow key={row.id} row={row} inDialog onOpen={onSelect} selected={selected?.id === row.id} disabled={loading} />)}
    {!loading && !error && !data?.rows.length && <p className="usage-empty-small">当前筛选范围暂无会话</p>}
    {pages > 1 && <div className="usage-pagination"><span>共 {data?.rowCount || 0} 个会话</span><div><button type="button" aria-label="项目会话上一页" disabled={loading || previous || page <= 1} onClick={() => setPage(page - 1)}><ChevronLeft size={14} /></button><span>{data?.page || page} / {pages}</span><button type="button" aria-label="项目会话下一页" disabled={loading || previous || page >= pages} onClick={() => setPage(page + 1)}><ChevronRight size={14} /></button></div></div>}
  </div>;
}

function ProjectGroup({ project, query, open, onToggle, selected, onSelect }) {
  const id = useId();
  const Icon = open ? FolderOpen : FolderClosed;
  return <div className="usage-project-group">
    <button type="button" className="usage-project-header" onClick={onToggle} aria-expanded={open} aria-controls={id}>
      <ChevronDown size={14} className="usage-project-fold" data-open={open} /><Icon size={18} className="usage-project-folder" />
      <span className="usage-project-name"><span><strong>{projectName(project.key)}</strong><Tooltip content={project.key ? <div className="space-y-1"><div>项目目录</div><div>{project.key}</div></div> : null} className="block min-w-0"><small className="block">{projectParent(project.key)}</small></Tooltip></span><span className="usage-project-latest">{project.title || '暂无会话标题'}</span></span>
      <span className="usage-project-summary"><time dateTime={project.lastAt}>{usageDate(project.lastAt)}</time><small>{count(project.sessions)} 个会话 · {count(project.tokens)} Token</small></span>
    </button>
    {open && <div id={id} role="region" aria-label={`${projectName(project.key)} 的会话`}><ProjectSessions project={project} query={query} selected={selected} onSelect={onSelect} /></div>}
  </div>;
}

export default function UsageProjectDialog({ data, query, initialSession, onClose }) {
  const [selected, setSelected] = useState(initialSession || null);
  const [expanded, setExpanded] = useState(initialSession ? initialSession.project || unrecordedProject : null);
  const [filter, setFilter] = useState('');
  const dialogRef = useRef(null);
  const closeRef = useRef(null);
  const headingId = useId();
  useEffect(() => {
    const previousFocus = document.activeElement;
    const oldOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    closeRef.current?.focus();
    const handleKey = event => {
      if (event.key === 'Escape') { event.preventDefault(); onClose(); }
      if (event.key !== 'Tab') return;
      const nodes = [...dialogRef.current.querySelectorAll('button:not(:disabled), input, [tabindex="0"]')];
      const first = nodes[0], last = nodes.at(-1);
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    };
    document.addEventListener('keydown', handleKey);
    return () => { document.removeEventListener('keydown', handleKey); document.body.style.overflow = oldOverflow; previousFocus?.focus(); };
  }, [onClose]);
  const projects = [...(data.projects || [])].filter(project => !filter || `${project.key} ${project.title}`.toLowerCase().includes(filter.toLowerCase())).sort((a, b) => (b.lastAt || '').localeCompare(a.lastAt || '') || a.key.localeCompare(b.key));
  return renderModalPortal(<div className="usage-project-overlay" onMouseDown={event => { if (event.target === event.currentTarget) onClose(); }}>
    <section ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={headingId} className="usage-project-dialog">
      <header className="usage-project-dialog-heading"><span className="usage-session-icon"><FolderOpen size={20} /></span><div><h2 id={headingId}>按项目查看会话</h2><p>当前筛选范围 · {data.projects.length} 个项目 · {count(data.summary.sessions)} 个会话</p></div><button ref={closeRef} type="button" onClick={onClose} aria-label="关闭项目会话明细"><X size={18} /></button></header>
      <div className="usage-project-toolbar"><label className="usage-search"><Search size={14} /><input aria-label="筛选项目" placeholder="搜索项目或最近会话标题" value={filter} onChange={event => setFilter(event.target.value)} /></label><span>展开项目查看会话与用量</span></div>
      <div className="usage-project-dialog-scroll">
        {selected && <div className="usage-session-facts" aria-label="选中会话用量"><div className="usage-session-facts-heading"><strong>{sessionTitle(selected)}</strong><button type="button" onClick={() => setSelected(null)} aria-label="收起选中会话详情"><X size={14} /></button></div><div className="usage-session-facts-grid">{[['输入', count(selected.input)], ['输出', count(selected.output)], ['缓存读取 / 写入', `${count(selected.cacheRead)} / ${count(selected.cacheCreation)}`], ['调用 / 活跃', `${count(selected.requests)} 次 · ${duration(selected.activeSeconds)}`], ['估算费用', money(selected.cost, selected.unpriced)], ['最近活跃', usageDate(selected.lastAt)]].map(([label, value]) => <div key={label}><small>{label}</small><span>{value}</span></div>)}</div><p>{selected.project || '未记录项目'} · {selected.models.join(' / ') || '未记录模型'}</p></div>}
        <div className="usage-project-list">{projects.map(project => { const key = project.key || unrecordedProject; return <ProjectGroup key={key} project={project} query={query} open={expanded === key} onToggle={() => setExpanded(expanded === key ? null : key)} selected={selected} onSelect={setSelected} />; })}{!projects.length && <div className="usage-empty-small">没有匹配的项目</div>}</div>
      </div>
    </section>
  </div>);
}
