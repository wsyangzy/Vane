export const buildInitialModelMappingRows = (modelMapping) => {
  const entries = Object.entries(modelMapping || {})
    .map(([sourceModel, targetModel]) => ({
      sourceModel: String(sourceModel || ''),
      targetModel: String(targetModel || '')
    }))
    .filter((row) => row.sourceModel.trim() || row.targetModel.trim());

  return entries.length > 0 ? entries : [{ sourceModel: '', targetModel: '' }];
};

export const buildModelMappingPayload = (rows) => {
  return (rows || []).reduce((mapping, row) => {
    const sourceModel = String(row?.sourceModel || '').trim();
    const targetModel = String(row?.targetModel || '').trim();
    if (sourceModel && targetModel) {
      mapping[sourceModel] = targetModel;
    }
    return mapping;
  }, {});
};
