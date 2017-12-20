import _ from 'lodash';
import React from 'react';
import { rowGutter } from './util/Utils.js';
import TabbedMetricsTable from './TabbedMetricsTable.jsx';
import { Col, Row } from 'antd';

const maxTsToFetch = 15;
export default class UpstreamDownstreamTables extends React.Component {
  render() {
    let numUpstreams = _.size(this.props.upstreamMetrics);
    let numDownstreams = _.size(this.props.downstreamMetrics);
    return (
      <Row gutter={rowGutter}>
        <Col span={24}>
          {
            numUpstreams === 0 ? null :
              <div className="upstream-downstream-list">
                <div className="border-container border-neutral subsection-header">
                  <div className="border-container-content subsection-header">
                Upstream {this.props.resource}s: {numUpstreams}
                  </div>
                </div>
                <TabbedMetricsTable
                  resource={`upstream_${this.props.resource}`}
                  entity={this.props.entity}
                  hideSparklines={numUpstreams > maxTsToFetch}
                  lastUpdated={this.props.lastUpdated}
                  metrics={this.props.upstreamMetrics}
                  metricsWindow={this.props.metricsWindow}
                  pathPrefix={this.props.pathPrefix} />
              </div>
          }
          {
            numDownstreams === 0 ? null :
              <div className="upstream-downstream-list">
                <div className="border-container border-neutral subsection-header">
                  <div className="border-container-content subsection-header">
                Downstream {this.props.resource}s: {numDownstreams}
                  </div>
                </div>
                <TabbedMetricsTable
                  resource={`downstream_${this.props.resource}`}
                  entity={this.props.entity}
                  hideSparklines={numDownstreams > maxTsToFetch}
                  lastUpdated={this.props.lastUpdated}
                  metrics={this.props.downstreamMetrics}
                  metricsWindow={this.props.metricsWindow}
                  pathPrefix={this.props.pathPrefix} />
              </div>
          }
        </Col>
      </Row>
    );
  }
}
