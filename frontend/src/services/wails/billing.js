import { parseNonNegativeNumber } from './mapping.js';
import { WailsApp, initWails } from './connection.js';

const getCredentialGroupDailyUsage = async (provider, channel, groupName, limitUSD = 0) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const usage = await WailsApp.GetCredentialGroupDailyUsage(
    provider,
    channel,
    groupName,
    parseNonNegativeNumber(limitUSD)
  );
  return {
    provider: usage?.provider || provider,
    channel: usage?.channel || channel,
    groupName: usage?.group_name || usage?.groupName || groupName,
    usedUSD: parseNonNegativeNumber(usage?.used_usd ?? usage?.usedUSD),
    limitUSD: parseNonNegativeNumber(usage?.limit_usd ?? usage?.limitUSD ?? limitUSD),
    remainingUSD: parseNonNegativeNumber(usage?.remaining_usd ?? usage?.remainingUSD),
    reached: Boolean(usage?.reached),
    resetAt: usage?.reset_at || usage?.resetAt || ''
  };
};

export { getCredentialGroupDailyUsage };
