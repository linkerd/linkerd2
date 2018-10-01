import _ from 'lodash';
import OctopusArms from './util/OctopusArms.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import { Col, Progress, Row } from 'antd';
import { displayName, metricToFormatter } from './util/Utils.js';
import { getSuccessRateClassification, srArcClassLabels } from './util/MetricUtils.jsx' ;
import './../../css/octopus.css';

const maxNumNeighbors = 6; // max number of neighbor nodes to show in the octopus graph

const Metric = ({title, value, className}) => {
  return (
    <Row type="flex" justify="center" className={`octopus-metric ${className}`}>
      <Col span={12} className="octopus-metric-title"><div>{title}</div></Col>
      <Col span={12} className="octopus-metric-value"><div>{value}</div></Col>
    </Row>
  );
};
Metric.defaultProps = { className: "" };
Metric.propTypes = {
  className: PropTypes.string,
  title: PropTypes.string.isRequired,
  value: PropTypes.string.isRequired
};

export default class Octopus extends React.Component {
  static defaultProps = {
    neighbors: {},
    resource: {},
    unmeshedSources: []
  }

  static propTypes = {
    neighbors: PropTypes.shape({}),
    resource: PropTypes.shape({}),
    unmeshedSources: PropTypes.arrayOf(PropTypes.shape({})),
  }

  getNeighborDisplayData = neighbors => {
    // only display maxNumNeighbors neighboring nodes in the octopus graph,
    // otherwise it will be really tall
    let upstreams = _.sortBy(neighbors.upstream, "resource.successRate");
    let downstreams = _.sortBy(neighbors.downstream, "resource.successRate");

    let display = {
      upstreams: {
        displayed: upstreams,
        collapsed: []
      },
      downstreams: {
        displayed: downstreams,
        collapsed: []
      }
    };

    if (_.size(upstreams) > maxNumNeighbors) {
      display.upstreams.displayed = _.take(upstreams, maxNumNeighbors);
      display.upstreams.collapsed = _.slice(upstreams, maxNumNeighbors, _.size(upstreams));
    }

    if (_.size(downstreams) > maxNumNeighbors) {
      display.downstreams.displayed = _.take(downstreams, maxNumNeighbors);
      display.downstreams.collapsed = _.slice(downstreams, maxNumNeighbors, _.size(downstreams));
    }

    return display;
  }

  linkedResourceTitle = (resource, display) => {
    return _.isNil(resource.namespace) ? display :
    <this.props.api.ResourceLink
      resource={resource}
      linkText={display} />;
  }

  renderResourceSummary(resource, type) {
    let display = displayName(resource);
    return (
      <div key={resource.name} className={`octopus-body ${type}`} title={display}>
        <div className={`octopus-title ${type}-title`}>
          { this.linkedResourceTitle(resource, display) }
        </div>
        <div>
          <div className="octopus-sr-gauge">
            <Progress
              className={`success-rate-arc ${getSuccessRateClassification(resource.successRate, srArcClassLabels)}`}
              type="dashboard"
              format={() => metricToFormatter["SUCCESS_RATE"](resource.successRate)}
              width={type === "main" ? 132 : 64}
              percent={resource.successRate * 100}
              gapDegree={180} />
          </div>
          <Metric title="RPS" value={metricToFormatter["REQUEST_RATE"](resource.requestRate)} />
          <Metric title="P99" value={metricToFormatter["LATENCY"](_.get(resource, "latency.P99"))} />
        </div>
      </div>
    );
  }

  renderUnmeshedResources = unmeshedResources => {
    return (
      <div key="unmeshed-resources" className="octopus-body neighbor unmeshed">
        <div className="octopus-title neighbor-title">Unmeshed</div>
        {
          _.map(unmeshedResources, r => {
            let display = displayName(r);
            return <div key={display} title={display}>{display}</div>;
          })
        }
      </div>
    );
  }

  renderCollapsedNeighbors = neighbors => {
    return (
      <div key="unmeshed-resources" className="octopus-body neighbor collapsed">
        {
          _.map(neighbors, r => {
            let display = displayName(r);
            return <div className="octopus-title neighbor-title" key={display}>{this.linkedResourceTitle(r, display)}</div>;
          })
        }
      </div>
    );
  }

  renderArrowCol = (numNeighbors, isOutbound) => {
    let baseHeight = 180;
    let width = 75;
    let showArrow = numNeighbors > 0;
    let isEven = numNeighbors % 2 === 0;
    let middleElementIndex = isEven ? ((numNeighbors - 1) / 2) : _.floor(numNeighbors / 2);

    let arrowTypes = _.map(_.times(numNeighbors), i => {
      if (i < middleElementIndex) {
        let height = (_.ceil(middleElementIndex - i) - 1) * baseHeight + (baseHeight / 2);
        return { type: "up", inboundType: "down", height };
      } else if (i === middleElementIndex) {
        return { type: "flat", inboundType: "flat", height: baseHeight };
      } else {
        let height = (_.ceil(i - middleElementIndex) - 1) * baseHeight + (baseHeight / 2);
        return { type: "down", inboundType: "up", height };
      }
    });

    let height = numNeighbors * baseHeight;
    let svg = (
      <svg height={height} width={width} version="1.1" viewBox={`0 0 ${width} ${height}`}>
        <defs />
        {
          _.map(arrowTypes, arrow => {
            let arrowType = isOutbound ? arrow.type : arrow.inboundType;
            return OctopusArms[arrowType](width, height, arrow.height, isOutbound);
          })
        }
      </svg>
    );

    return !showArrow ? null : svg;
  }

  render() {
    let { resource, neighbors, unmeshedSources } = this.props;

    if (_.isEmpty(resource)) {
      return null;
    }

    let display = this.getNeighborDisplayData(neighbors);

    let numUpstreams = _.size(display.upstreams.displayed) + (_.isEmpty(unmeshedSources) ? 0 : 1) +
      (_.isEmpty(display.upstreams.collapsed) ? 0 : 1);
    let hasUpstreams = numUpstreams > 0;
    let numDownstreams = _.size(display.downstreams.displayed) + (_.isEmpty(display.downstreams.collapsed) ? 0 : 1);
    let hasDownstreams = numDownstreams > 0;

    return (
      <div className="octopus-graph">
        <Row type="flex" justify="center" align="middle">
          <Col span={6} className={`octopus-col ${hasUpstreams ? "resource-col" : ""}`}>
            {_.map(display.upstreams.displayed, n => this.renderResourceSummary(n, "neighbor"))}
            {_.isEmpty(unmeshedSources) ? null : this.renderUnmeshedResources(unmeshedSources)}
            {_.isEmpty(display.upstreams.collapsed) ? null : this.renderCollapsedNeighbors(display.upstreams.collapsed)}
          </Col>

          <Col span={2} className="octopus-col">
            {this.renderArrowCol(numUpstreams, false)}
          </Col>

          <Col span={8} className="octopus-col resource-col">
            {this.renderResourceSummary(resource, "main")}
          </Col>

          <Col span={2} className="octopus-col">
            {this.renderArrowCol(numDownstreams, true)}
          </Col>

          <Col span={6} className={`octopus-col ${hasDownstreams ? "resource-col" : ""}`}>
            {_.map(display.downstreams.displayed, n => this.renderResourceSummary(n, "neighbor"))}
            {_.isEmpty(display.downstreams.collapsed) ? null : this.renderCollapsedNeighbors(display.downstreams.collapsed)}
          </Col>
        </Row>
      </div>
    );
  }
}
