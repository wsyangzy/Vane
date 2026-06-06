// ============================================
// ActiveGroupSwitcher - 渠道切换器
// 同时显示 Claude 和 Codex 各自的活跃渠道，独立切换
// ============================================

import { useState, useMemo, useRef, useEffect } from 'react'
import { Check, AlertCircle, Loader2, Server, ChevronDown } from 'lucide-react'

const ActiveGroupSwitcher = ({
  groups = [],
  activeGroup = '',
  onSwitch,
  loading = false
}) => {
  const [isOpen, setIsOpen] = useState(false)
  const [switching, setSwitching] = useState(null)
  const [switchError, setSwitchError] = useState('')
  const switchingRef = useRef(false)
  const containerRef = useRef(null)

  const sortedGroups = useMemo(() => {
    return [...groups].sort((a, b) => (a.priority ?? 999) - (b.priority ?? 999))
  }, [groups])

  const claudeChannels = useMemo(() => sortedGroups.filter(g => g.provider !== 'codex'), [sortedGroups])
  const codexChannels = useMemo(() => sortedGroups.filter(g => g.provider === 'codex'), [sortedGroups])

  // 各 provider 的活跃渠道（通过 is_active 或 activeGroup 判断）
  const activeClaudeChannel = useMemo(() => {
    return claudeChannels.find(g => g.is_active || g.name === activeGroup) || claudeChannels[0]
  }, [claudeChannels, activeGroup])

  const activeCodexChannel = useMemo(() => {
    return codexChannels.find(g => g.is_active) || codexChannels[0]
  }, [codexChannels])

  useEffect(() => {
    const handler = (e) => {
      if (containerRef.current && !containerRef.current.contains(e.target)) setIsOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const handleSelect = async (channel) => {
    if (switchingRef.current || switching || loading) return
    const isAlreadyActive = channel.provider === 'codex'
      ? channel.name === activeCodexChannel?.name
      : channel.name === activeClaudeChannel?.name
    if (isAlreadyActive) return
    const switchingKey = `${channel.provider || 'claude'}:${channel.name}`
    switchingRef.current = true
    setSwitchError('')
    setSwitching(switchingKey)
    try {
      await onSwitch?.(channel.name, null, channel.provider)
    } catch (error) {
      const msg = error?.message || error?.msg || String(error)
      setSwitchError(msg)
    } finally {
      setSwitching(null)
      switchingRef.current = false
    }
  }

  if (!groups.length) {
    return (
      <div className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm text-gray-400 bg-gray-50 border border-gray-200">
        <AlertCircle className="w-3.5 h-3.5" />
        <span>无可用渠道</span>
      </div>
    )
  }

  const renderPills = (channels, color) => {
    const styles = color === 'orange'
      ? { active: 'bg-orange-500 text-white border-orange-500 shadow-sm', idle: 'bg-white text-slate-600 border-slate-200 hover:border-orange-300 hover:text-orange-600' }
      : { active: 'bg-blue-500 text-white border-blue-500 shadow-sm', idle: 'bg-white text-slate-600 border-slate-200 hover:border-blue-300 hover:text-blue-600' }

    return channels.map(channel => {
      const isActive = color === 'orange'
        ? channel.name === activeClaudeChannel?.name
        : channel.name === activeCodexChannel?.name
      const isSwitching = switching === `${channel.provider || 'claude'}:${channel.name}`
      const isBusy = Boolean(switching) || loading
      return (
        <button
          key={`${channel.provider || color}:${channel.name}`}
          onClick={() => handleSelect(channel)}
          disabled={isBusy || isActive}
          className={`inline-flex items-center gap-1 px-2.5 py-1 text-xs font-medium rounded-full border transition-all whitespace-nowrap ${
            isActive ? styles.active : styles.idle
          } ${isBusy ? 'opacity-60 cursor-wait' : ''}`}
        >
          {isSwitching && <Loader2 className="w-3 h-3 animate-spin" />}
          {channel.name}
          {isActive && <Check className="w-3 h-3" />}
        </button>
      )
    })
  }

  return (
    <div className="relative" ref={containerRef}>
      <button
        onClick={() => { setSwitchError(''); setIsOpen(!isOpen) }}
        disabled={switching || loading}
        className={`flex items-center gap-2 px-3 py-1.5 bg-white border rounded-lg text-sm font-medium transition-all shadow-sm ${
          isOpen
            ? 'border-indigo-300 ring-2 ring-indigo-100 text-indigo-700'
            : 'border-gray-200 text-gray-700 hover:border-indigo-300 hover:text-indigo-600'
        } ${(switching || loading) ? 'opacity-60 cursor-wait' : ''}`}
      >
        {claudeChannels.length > 0 && (
          <span className="flex items-center gap-1">
            <span className="text-[10px] text-orange-500 font-bold">Claude:</span>
            <span className="text-xs font-bold text-orange-600">{activeClaudeChannel?.name || '-'}</span>
          </span>
        )}
        {claudeChannels.length > 0 && codexChannels.length > 0 && (
          <span className="text-slate-300">|</span>
        )}
        {codexChannels.length > 0 && (
          <span className="flex items-center gap-1">
            <span className="text-[10px] text-blue-500 font-bold">Codex:</span>
            <span className="text-xs font-bold text-blue-600">{activeCodexChannel?.name || '-'}</span>
          </span>
        )}
        <ChevronDown className={`w-3.5 h-3.5 text-gray-400 transition-transform ${isOpen ? 'rotate-180' : ''}`} />
      </button>

      {isOpen && (
        <div className="absolute top-full left-0 mt-2 bg-white rounded-xl shadow-xl border border-gray-100 ring-1 ring-black/5 z-50 overflow-hidden min-w-[240px]">
          {switchError && (
            <div className="px-3 py-2 bg-rose-50 border-b border-rose-100 text-xs text-rose-700 flex items-start gap-2">
              <AlertCircle className="w-3.5 h-3.5 mt-0.5 shrink-0" />
              <span>{switchError}</span>
            </div>
          )}
          <div className="p-3 space-y-3">
            {claudeChannels.length > 0 && (
              <div>
                <div className="text-[10px] font-bold text-orange-600 uppercase tracking-wider mb-1.5">Claude</div>
                <div className="flex flex-wrap gap-1.5">
                  {renderPills(claudeChannels, 'orange')}
                </div>
              </div>
            )}
            {codexChannels.length > 0 && (
              <div>
                <div className="text-[10px] font-bold text-blue-600 uppercase tracking-wider mb-1.5">Codex</div>
                <div className="flex flex-wrap gap-1.5">
                  {renderPills(codexChannels, 'blue')}
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

export default ActiveGroupSwitcher
