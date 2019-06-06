import PropTypes from 'prop-types';
import _each from 'lodash/each';
import _isEmpty from 'lodash/isEmpty';
import _startsWith from 'lodash/startsWith';

export const processEdges = (rawEdges, resourceName) => {
  let edges = [];
  if (_isEmpty(rawEdges) || _isEmpty(rawEdges.ok) || _isEmpty(rawEdges.ok.edges)) {
    return edges;
  }
  _each(rawEdges.ok.edges, edge => {
    if (_isEmpty(edge)) {
      return;
    }
    // check if any of the returned edges match the current resourceName
    if (_startsWith(edge.src.name, resourceName) || _startsWith(edge.dst.name, resourceName)) {
      edge.key = edge.src.name + edge.dst.name;
      edges.push(edge);
    }
  });
  return edges;
};

export const processedEdgesPropType = PropTypes.shape({
  clientId: PropTypes.string,
  dst: PropTypes.shape(edgeResourcePropType).isRequired,
  key: PropTypes.string.isRequired,
  noIdentityMsg: PropTypes.string,
  serverId: PropTypes.string,
  src: PropTypes.shape(edgeResourcePropType).isRequired,
});

const edgeResourcePropType = PropTypes.shape({
  name: PropTypes.string,
  type: PropTypes.string
});
