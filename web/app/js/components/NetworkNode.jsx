import _ from 'lodash';
import PropTypes from 'prop-types';
import React from 'react';
// import { withContext } from './util/AppContext.jsx';

import withREST from './util/withREST.jsx';
import { metricsPropType, processSingleResourceRollup } from './util/MetricUtils.js';

class NetworkNode extends React.Component {
  static propTypes = {
    data: PropTypes.arrayOf(metricsPropType.isRequired).isRequired,
    name: PropTypes.string.isRequired,
  }

  processData() {
    const {data} = this.props;

    let processedMetrics = [];
    if (_.has(data, '[0].ok')) {
      processedMetrics = processSingleResourceRollup(data[0]);
    }
    return processedMetrics;
  }

  showRelationship(connections) {
    let nodes = _.map(connections, "name");
    return nodes;
  }

  render() {
    let connections = this.processData();
    let relationships = this.showRelationship(connections);

    return (
      <div>
        {this.props.name}
        {_.size(relationships) > 0 ? " -> " : ""}
        {_.join(relationships, ", ")}
      </div>
    );
  }

}

export default withREST(NetworkNode, ({api, name, namespace}) => [api.fetchMetrics(api.urlsForResource("deployments", `${namespace}&from_name=${name}`))], );
