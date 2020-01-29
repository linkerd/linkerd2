import PropTypes from 'prop-types';
import _each from 'lodash/each';
import _isEmpty from 'lodash/isEmpty';

export const processEdges = (rawEdges, resourceName) => {
  const edges = [];
  if (_isEmpty(rawEdges) || _isEmpty(rawEdges.ok) || _isEmpty(rawEdges.ok.edges)) {
    return edges;
  }
  _each(rawEdges.ok.edges, edge => {
    const edge_ = edge;
    if (_isEmpty(edge_)) {
      return;
    }
    // check if any of the returned edge_s match the current resourceName
    if (edge_.src.name === resourceName) {
      // current resource is SRC
      edge_.direction = 'OUTBOUND';
      edge_.identity = edge_.serverId;
      edge_.name = edge_.dst.name;
      edge_.namespace = edge_.dst.namespace;
      edge_.key = edge_.src.name + edge_.dst.name;
      edges.push(edge_);
    } else if (edge_.dst.name === resourceName) {
      // current resource is DST
      edge_.direction = 'INBOUND';
      edge_.identity = edge_.clientId;
      edge_.name = edge_.src.name;
      edge_.namespace = edge_.src.namespace;
      edge_.key = edge_.src.name + edge_.dst.name;
      edges.push(edge_);
    }
  });
  return edges;
};

export const processedEdgesPropType = PropTypes.shape({
  dst: PropTypes.shape({
    name: PropTypes.string,
    namespace: PropTypes.string,
    type: PropTypes.string,
  }),
  clientId: PropTypes.string,
  direction: PropTypes.string,
  identity: PropTypes.string,
  key: PropTypes.string.isRequired,
  name: PropTypes.string,
  noIdentityMsg: PropTypes.string,
  serverId: PropTypes.string,
  src: PropTypes.shape({
    name: PropTypes.string,
    namespace: PropTypes.string,
    type: PropTypes.string,
  }),
});
