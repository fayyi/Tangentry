UPDATE agent_operations
SET error=CASE WHEN state='unsupported' THEN '当前能力不可用。' ELSE '操作失败；详细错误未保存。' END
WHERE state IN ('failed','unsupported')
  AND error NOT IN (
    '操作失败；详细错误未保存。',
    '当前能力不可用。',
    'Tangentry 在操作完成前退出，操作未恢复。'
  );
