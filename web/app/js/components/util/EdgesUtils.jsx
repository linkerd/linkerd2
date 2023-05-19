import PropTypes from 'prop-types';
import _each from 'lodash/each';
import _isEmpty from 'lodash/isEmpty';

export const processEdges = (rawEdges, resourceName) => {
  const edges = [];
  if (_isEmpty(rawEdges) || _isEmpty(rawEdges.ok) || _isEmpty(rawEdges.ok.edges)) {
    return edges;
  }
  _each(rawEdges.ok.edges, edge => {
    if (_isEmpty(edge)) {
      return;
    }
    // check if any of the returned edges match the current resourceName
    if (edge.src.name === resourceName) {
      // current resource is SRC
      edge.direction = 'OUTBOUND';
      edge.identity = edge.serverId;
      edge.name = edge.dst.name;
      edge.namespace = edge.dst.namespace;
      edge.key = edge.src.name + edge.dst.name;
      edges.push(edge);
    } else if (edge.dst.name === resourceName) {
      // current resource is DST
      edge.direction = 'INBOUND';
      edge.identity = edge.clientId;
      edge.name = edge.src.name;
      edge.namespace = edge.src.namespace;
      edge.key = edge.src.name + edge.dst.name;
      edges.push(edge);
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
