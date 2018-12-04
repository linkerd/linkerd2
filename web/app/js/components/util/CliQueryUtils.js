import _ from 'lodash';

const topRoutesDisplayOrder = [
  "namespace",
  "from",
  "from_name",
  "from_type",
  "from_namespace"
];


const tapDisplayOrder = query => _.compact([
  _.isNil(query.resource) ? null : query.resource.indexOf("namespace") === 0 ? null : "namespace",
  "toResource",
  _.isNil(query.toResource) ? null : query.toResource.indexOf("namespace") === 0 ? null : "toNamespace",
  "method",
  "path",
  "scheme",
  "authority",
  "maxRps"
]);

export const displayOrder = (cmd, query) => {
  if (cmd === "tap" || cmd === "top") {
    return tapDisplayOrder(query);
  }
  if (cmd === "routes") {
    return topRoutesDisplayOrder;
  }
  return [];
};
