import _ from 'lodash';
import PropTypes from 'prop-types';
import React from 'react';
import { Col, Popover, Row } from 'antd';
import { metricToFormatter, toShortResourceName } from './util/Utils.js';
import './../../css/octopus.css';

const displayName = resource => `${toShortResourceName(resource.type)}/${resource.name}`;

const getDotClassification = sr => {
  if (sr < 0.9) {
    return "status-dot-poor";
  } else if (sr < 0.95) {
    return "status-dot-ok";
  } else {return "status-dot-good";}
};

const Neighbor = ({neighbor, direction}) => {
  return (
    <div className="neighbor">
      <Popover
        title={displayName(neighbor)}
        content={<MetricSummaryRow resource={neighbor} metricClass="metric-sm" />}
        placement={direction ==="in" ? "left" : "right"}>
        <div className="neighbor-row">
          <div>{direction === "in" ? "<" : ">"}</div>
          <div className={`status-dot ${getDotClassification(neighbor.successRate)}`} />
          <div>{displayName(neighbor)}</div>
        </div>
      </Popover>
    </div>
  );
};
Neighbor.propTypes = {
  direction: PropTypes.string.isRequired,
  neighbor: PropTypes.shape({}).isRequired
};

const Metric = ({title, value, metricClass}) => {
  return (
    <Row type="flex" justify="center" className={`octopus-${metricClass}`}>
      <Col span={12} className="octopus-metric-title"><div>{title}</div></Col>
      <Col span={12} className="octopus-metric-value"><div>{value}</div></Col>
    </Row>
  );
};
Metric.propTypes = {
  metricClass: PropTypes.string.isRequired,
  title: PropTypes.string.isRequired,
  value: PropTypes.string.isRequired
};

const MetricSummaryRow = ({resource, metricClass}) => {
  return (
    <React.Fragment>
      <Metric title="Success Rate" value={metricToFormatter["SUCCESS_RATE"](resource.successRate)} metricClass={metricClass} />
      <Metric title="Request Rate" value={metricToFormatter["REQUEST_RATE"](resource.requestRate)} metricClass={metricClass} />
      <Metric title="P99 Latency" value={metricToFormatter["LATENCY"](_.get(resource, "latency.P99"))} metricClass={metricClass}  />
    </React.Fragment>
  );
};
MetricSummaryRow.propTypes = {
  metricClass: PropTypes.string.isRequired,
  resource: PropTypes.shape({}).isRequired
};

export default class Octopus extends React.Component {
  static defaultProps = {
    metrics: {},
    neighbors: {}
  }
  static propTypes = {
    metrics: PropTypes.shape({}),
    neighbors: PropTypes.shape({}),
    resource: PropTypes.shape({}).isRequired
  }

  render() {
    let { resource, metrics, neighbors } = this.props;

    return (
      <div className="octopus-container">
        <div className="octopus-graph">
          <h1 className="octopus-title">{displayName(resource)}</h1>
          <MetricSummaryRow resource={metrics} metricClass="metric-lg" />
          <hr />
          <Row type="flex" justify="center">
            <Col span={12} className="octopus-upstreams">
              {_.map(neighbors.upstream, n => <Neighbor neighbor={n} direction="in" key={n.namespace + "-" + n.name} />)}
            </Col>
            <Col span={12} className="octopus-downstreams">
              {_.map(neighbors.downstream, n => <Neighbor neighbor={n} direction="out" key={n.namespace + "-" + n.name} />)}
            </Col>
          </Row>
        </div>
      </div>
    );
  }
}
