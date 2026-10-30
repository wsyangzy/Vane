import { getErrorMessage } from '../../../shared/lib/errorMessage.js';

// Keep failed cleanup across retries so another submission cannot create a second channel.
export const createChannelSetupCommitter = () => {
  let pendingCleanup = null;
  let saving = false;

  return async ({ create, find, saveGroup, saveEndpoint, remove }) => {
    if (saving) throw new Error('正在创建渠道，请稍候');
    saving = true;
    try {
      if (pendingCleanup) {
        try {
          await pendingCleanup();
          pendingCleanup = null;
        } catch (error) {
          throw new Error(`上次创建的渠道尚未清理，请重试或刷新列表后处理。${getErrorMessage(error, '清理失败')}`);
        }
      }

      // Register cleanup only after a successful create; a duplicate-name error must not delete an existing channel.
      await create();
      let channel = null;
      pendingCleanup = async () => remove(channel || await find());
      try {
        channel = await find();
        if (!channel) throw new Error('未能读取新建渠道');
        const group = await saveGroup(channel);
        if (!(Number(group?.id) > 0)) throw new Error('未能读取新建凭证分组');
        await saveEndpoint(channel, group);
      } catch (error) {
        try {
          await pendingCleanup();
          pendingCleanup = null;
        } catch (cleanupError) {
          throw new Error(`创建失败，自动清理未完成；再次提交将先重试清理。${getErrorMessage(error, '保存失败')}；${getErrorMessage(cleanupError, '清理失败')}`);
        }
        throw new Error(`创建失败，已撤销本次新增渠道，可修改后重试。${getErrorMessage(error, '保存失败')}`);
      }
      pendingCleanup = null;
    } finally {
      saving = false;
    }
  };
};
