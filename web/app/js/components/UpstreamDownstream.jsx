import _ from 'lodash';
import React from 'react';
import { rowGutter } from './util/Utils.js';
import TabbedMetricsTable from './TabbedMetricsTable.jsx';
import { Col, Row } from 'antd';

export default class UpstreamDownstreamTables extends React.Component {
  render() {
    return (
      <Row gutter={rowGutter}>
        <Col span={24}>
          <div className="upstream-downstream-list">
            <div className="border-container border-neutral subsection-header">
              <div className="border-container-content subsection-header">
                Upstream {this.props.entity}s: {_.size(this.props.upstreamMetrics)}
              </div>
            </div>
            <TabbedMetricsTable
              resource={`upstream_${this.props.entity}`}
              lastUpdated={this.props.lastUpdated}
              metrics={this.props.upstreamMetrics}
              pathPrefix={this.props.pathPrefix} />
          </div>
          <div className="upstream-downstream-list">
            <div className="border-container border-neutral subsection-header">
              <div className="border-container-content subsection-header">
                Downstream {this.props.entity}s: {_.size(this.props.downstreamMetrics)}
              </div>
            </div>
            <TabbedMetricsTable
              resource={`downstream_${this.props.entity}`}
              lastUpdated={this.props.lastUpdated}
              metrics={this.props.downstreamMetrics}
              pathPrefix={this.props.pathPrefix} />
          </div>
        </Col>
      </Row>
    );
  }
}
