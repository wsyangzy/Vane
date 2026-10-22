// ============================================
// Pagination - 分页组件
// 2025-12-01 11:34:29
// ============================================

import { ChevronLeft, ChevronRight, MousePointer2 } from 'lucide-react';
import { CustomSelect, Tooltip } from '@/shared/ui/index.js';

/**
 * Pagination - 分页控件组件
 * @param {Object} props
 * @param {number} props.currentPage - 当前页码
 * @param {number} props.pageSize - 每页条数
 * @param {number} props.total - 总记录数
 * @param {Function} props.onPageChange - 页码变更回调
 * @param {Function} props.onPageSizeChange - 每页条数变更回调
 */
const Pagination = ({
  currentPage = 1,
  pageSize = 10,
  total = 0,
  onPageChange,
  onPageSizeChange
}) => {
  const totalPages = Math.ceil(total / pageSize);
  const startItem = total === 0 ? 0 : (currentPage - 1) * pageSize + 1;
  const endItem = Math.min(currentPage * pageSize, total);

  // 生成页码按钮数组
  const getPageNumbers = () => {
    const pages = [];
    const maxVisible = 7; // 最多显示页码数

    if (totalPages <= maxVisible) {
      // 总页数小于最大显示数，全部显示
      for (let i = 1; i <= totalPages; i++) {
        pages.push(i);
      }
    } else {
      // 总页数大于最大显示数，智能显示
      if (currentPage <= 3) {
        // 当前页在前面
        pages.push(1, 2, 3, 4, '...', totalPages);
      } else if (currentPage >= totalPages - 2) {
        // 当前页在后面
        pages.push(1, '...', totalPages - 3, totalPages - 2, totalPages - 1, totalPages);
      } else {
        // 当前页在中间
        pages.push(1, '...', currentPage - 1, currentPage, currentPage + 1, '...', totalPages);
      }
    }

    return pages;
  };

  const pageNumbers = getPageNumbers();

  // 每页条数选项
  const pageSizeOptions = [
    { value: 10, label: '10 条/页' },
    { value: 20, label: '20 条/页' },
    { value: 50, label: '50 条/页' },
    { value: 100, label: '100 条/页' }
  ];

  return (
    <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t border-gray-200 bg-gray-50/30 px-4 py-2.5">
      {/* 左侧信息 */}
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 text-xs text-gray-600 sm:text-sm">
        <span>
          显示第 <span className="font-medium">{startItem}</span> 到 <span className="font-medium">{endItem}</span> 条，
          共 <span className="font-medium">{total}</span> 条记录
        </span>
        <div className="flex items-center gap-2">
          <span className="text-gray-400">|</span>
          <CustomSelect
            options={pageSizeOptions}
            value={pageSize}
            onChange={(value) => onPageSizeChange?.(value)}
            size="sm"
            className="min-w-[100px]"
          />
        </div>
        <Tooltip content="单击请求行复制 ID · 双击查看详情">
          <span tabIndex={0} aria-label="请求列表操作提示" className="inline-flex items-center gap-1 rounded text-slate-400 focus-visible:outline-2 focus-visible:outline-indigo-500">
            <MousePointer2 className="h-3.5 w-3.5" />
            <span className="hidden text-xs min-[75rem]:inline">操作提示</span>
          </span>
        </Tooltip>
      </div>

      {/* 右侧页码导航 */}
      <div className="ml-auto flex shrink-0 items-center gap-1">
        {/* 上一页 */}
        <button
          aria-label="上一页"
          onClick={() => onPageChange?.(currentPage - 1)}
          disabled={currentPage <= 1}
          className="p-1.5 rounded-md border border-gray-200 bg-white text-gray-400 hover:text-indigo-600 hover:border-indigo-200 disabled:opacity-50 disabled:cursor-not-allowed transition-all"
        >
          <ChevronLeft className="w-4 h-4" />
        </button>

        {/* 页码按钮 */}
        <div className="flex items-center gap-1 mx-1">
          {pageNumbers.map((page, index) => {
            if (page === '...') {
              return (
                <span key={`ellipsis-${index}`} className="flex items-center justify-center w-8 h-8 text-gray-400">
                  ...
                </span>
              );
            }

            const isActive = page === currentPage;
            return (
              <button
                key={page}
                onClick={() => onPageChange?.(page)}
                className={`min-w-[32px] h-8 flex items-center justify-center rounded-md text-sm font-medium transition-colors ${
                  isActive
                    ? 'bg-indigo-600 text-white shadow-sm'
                    : 'bg-white border border-gray-200 text-gray-600 hover:bg-gray-50 hover:text-indigo-600'
                }`}
              >
                {page}
              </button>
            );
          })}
        </div>

        {/* 下一页 */}
        <button
          aria-label="下一页"
          onClick={() => onPageChange?.(currentPage + 1)}
          disabled={currentPage >= totalPages}
          className="p-1.5 rounded-md border border-gray-200 bg-white text-gray-600 hover:text-indigo-600 hover:border-indigo-200 disabled:opacity-50 disabled:cursor-not-allowed transition-all"
        >
          <ChevronRight className="w-4 h-4" />
        </button>
      </div>
    </div>
  );
};

export default Pagination;
