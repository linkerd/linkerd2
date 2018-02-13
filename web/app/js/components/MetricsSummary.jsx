import _ from 'lodash';
import { metricToFormatter } from './util/Utils.js';
import React from 'react';
import { Col, Row } from 'antd';
import './../../css/metric-summary.css';

export default class MetricsSummary extends React.Component {
  render() {
    return (
      <Row className="metric-summary">
        <Col span={4} className="metric metric-large">
          <div className="metric-title">Request rate</div>
          <div className="metric-value">
            {metricToFormatter["REQUEST_RATE"](this.props.metrics.requestRate)}
          </div>
        </Col>
        <Col span={4} className="metric metric-large">
          <div className="metric-title">Success rate</div>
          <div className="metric-value">
            {metricToFormatter["SUCCESS_RATE"](this.props.metrics.successRate)}
          </div>
        </Col>
        {
          _.map(['P50', 'P95', 'P99'], label => {
            let latency = _.get(this.props.metrics, ['latency', label]);

            return (
              <Col span={4} className="metric metric-large" key={`latency${label}`}>
                <div className="metric-title">{label} latency</div>
                <div className="metric-value">{metricToFormatter["LATENCY"](latency)}</div>
              </Col>
            );
          })
        }
      </Row>
    );
  }
}
