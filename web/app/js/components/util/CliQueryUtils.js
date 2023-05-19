import _compact from 'lodash/compact';
import _isNil from 'lodash/isNil';

const topRoutesDisplayOrder = query => _compact([
  'namespace',
  'toResource',
  _isNil(query.to_type) ? null : 'toNamespace',
]);

const tapDisplayOrder = query => _compact([
  _isNil(query.resource) ? null : query.resource.indexOf('namespace') === 0 ? null : 'namespace',
  'toResource',
  _isNil(query.toResource) ? null : query.toResource.indexOf('namespace') === 0 ? null : 'toNamespace',
  'method',
  'path',
  'scheme',
  'authority',
  'maxRps',
]);

export const displayOrder = (cmd, query) => {
  if (cmd === 'tap' || cmd === 'top') {
    return tapDisplayOrder(query);
  }
  if (cmd === 'routes') {
    return topRoutesDisplayOrder(query);
  }
  return [];
};
