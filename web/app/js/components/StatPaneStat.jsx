import _ from 'lodash';
import LineGraph from './LineGraph.jsx';
import React from 'react';
import { metricToFormatter, toClassName } from './util/Utils.js';

export default class StatPaneStat extends React.Component {
  render() {
    let lastDatapoint = _.last(this.props.timeseries) || {};
    let metric = _.get(lastDatapoint, "value");
    let displayMetric = metricToFormatter[this.props.metric](metric);

    return (
      <div className={`border-container border-neutral`}>
        <div className="border-container-content">
          <div className="summary-container clearfix">
            <div className="metric-info">
              <div className="summary-title">{this.props.name}</div>
              <div className="summary-info">Last 10 minutes performance</div>
            </div>
            <div className="metric-value">{displayMetric}</div>
          </div>

          <LineGraph
            data={this.props.timeseries}
            lastUpdated={this.props.lastUpdated}
            containerClassName={`stat-pane-stat-${toClassName(this.props.name)}`} />
        </div>
      </div>
    );
  }
}
