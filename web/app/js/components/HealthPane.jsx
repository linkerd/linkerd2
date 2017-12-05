import React from 'react';
import { Row, Col, Progress } from 'antd';
import Metric from './Metric.jsx';
import { rowGutter, metricToFormatter } from './util/Utils.js';
import styles from './../../css/health-pane.css';

const TrafficIndicator = () => {
  return (
    <Progress
      percent={100}
      status="active"
      strokeWidth={8}
      format={p => null}
    />
  );
}

export default class HealthPane extends React.Component {
  getRequestRate(metrics) {
    return _.sumBy(metrics, metric => {
      return _.get(metric, ['rollup', 'requestRate']);
    });
  }

  getAvgSuccessRate(metrics) {
    return _.meanBy(metrics, metric => {
      return _.get(metric, ['rollup', 'successRate']);
    })
  }

  getHealthClassName(successRate) {
    if (successRate < 0.4) {
      return "health-bad";
    }
    if (successRate > 0.95) {
      return "health-good";
    }
    return "health-neutral";
  }

  getHealthStats() {
    let inboundSr = this.getAvgSuccessRate(this.props.upstreamMetrics);
    let outboundSr = this.getAvgSuccessRate(this.props.downstreamMetrics);
    // let sr = _.get(this.props.metrics, [0, 'rollup', 'successRate'], 0);
    let sr = Math.random() // TODO: get actual metric

    return {
      inbound: {
        requests: metricToFormatter["REQUEST_RATE"](this.getRequestRate(this.props.upstreamMetrics)),
        health: this.getHealthClassName(inboundSr)
      },
      outbound: {
        requests: metricToFormatter["REQUEST_RATE"](this.getRequestRate(this.props.downstreamMetrics)),
        health: this.getHealthClassName(outboundSr)
      },
      current: {
        health: this.getHealthClassName(sr)
      }
    }
  }

  render() {
    let stats = this.getHealthStats();

    return (
      <div key="entity-heath" className="entity-health">
        <div className="subsection-header">{this.props.entityType} Health</div>
        <Row>
          <Col span={8}>
            <Metric title="Inbound request rate" value={stats.inbound.requests} className="float-right" />
          </Col>
          <Col span={8}></Col>
          <Col span={8}>
            <Metric title="Outbound request rate" value={stats.outbound.requests} className="float-left" />
          </Col>
        </Row>

        <Row>
          <Col span={8}>
            <div className="entity-count">&laquo; {_.size(this.props.upstreamMetrics)} {this.props.entityType}s</div>
            <div className={`adjacent-health ${stats.inbound.health}`}>
              <TrafficIndicator />
            </div>
          </Col>
          <Col span={8}>
            <div className="entity-count">&nbsp;</div>
            <div className={`entity-title ${stats.current.health}`}>{this.props.entity}</div>
          </Col>
          <Col span={8}>
            <div className="entity-count float-right">{_.size(this.props.downstreamMetrics)} {this.props.entityType}s &raquo;</div>
            <div className={`adjacent-health ${stats.outbound.health}`}>
              <TrafficIndicator />
            </div>
          </Col>
        </Row>
      </div>
    );
  }
}
