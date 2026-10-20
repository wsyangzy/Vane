// ============================================
// useTimeRange Hook - 时间范围快捷选择
// 2025-12-01 09:30:08
// ============================================

import { useState, useCallback } from 'react';
import { getTodayTimeRange } from './useFilters.js';

/**
 * 获取指定天数前的时间范围
 * @param {number} days - 天数
 * @returns {{ startDate: string, endDate: string }}
 */
const getTimeRangeByDays = (days) => {
  const now = new Date();
  const endOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 23, 59, 59);
  const startDate = new Date(now.getTime() - (days - 1) * 24 * 60 * 60 * 1000);
  startDate.setHours(0, 0, 0, 0);

  const formatDateTime = (date) => {
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    const hours = String(date.getHours()).padStart(2, '0');
    const minutes = String(date.getMinutes()).padStart(2, '0');
    return `${year}-${month}-${day}T${hours}:${minutes}`;
  };

  return {
    startDate: formatDateTime(startDate),
    endDate: formatDateTime(endOfToday)
  };
};

/**
 * useTimeRange Hook - 管理时间范围快捷选择
 * @param {Function} onRangeChange - 时间范围变更回调
 * @returns {Object}
 */
export const useTimeRange = (onRangeChange) => {
  const [activeRange, setActiveRange] = useState('today');

  // 选择时间范围
  const selectRange = useCallback((range) => {
    setActiveRange(range);

    let timeRange;
    switch (range) {
      case 'today':
        timeRange = getTodayTimeRange();
        break;
      case '7days':
        timeRange = getTimeRangeByDays(7);
        break;
      case '30days':
        timeRange = getTimeRangeByDays(30);
        break;
      default:
        timeRange = getTodayTimeRange();
    }

    // 通知父组件时间范围变更
    onRangeChange?.(timeRange);
  }, [onRangeChange]);

  const clearRange = useCallback(() => {
    setActiveRange('');
  }, []);

  return {
    activeRange,
    selectRange,
    clearRange
  };
};

export default useTimeRange;
