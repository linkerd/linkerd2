import _ from 'lodash';
import { metricToFormatter } from './util/Utils.js';
import React from 'react';
import * as d3 from 'd3';
import './../../css/scatterplot.css';

const defaultSvgWidth = 574;
const defaultSvgHeight = 375;
const margin = { top: 0, right: 0, bottom: 10, left: 0 };
const baseWidth = 8;
const circleRadius = 2 * baseWidth;
const graphPadding = 3 * circleRadius;
const highlightBarWidth = 3 * circleRadius;
const successRateColorScale = d3.scaleQuantize()
  .domain([0, 1])
  .range(["#8B0000", "#FF6347", "#FF4500", "#FFA500","#008000"]);

export default class ScatterPlot extends React.Component {
  constructor(props) {
    super(props);
    this.renderNodeTooltipDatum = this.renderNodeTooltipDatum.bind(this);
    this.state = this.getChartDimensions();
  }

  componentWillMount() {
    this.initializeScales();
  }

  componentDidMount() {
    this.svg = d3.select("." + this.props.containerClassName)
      .append("svg")
      .attr("class", "scatterplot")
      .attr("width", defaultSvgWidth)
      .attr("height", defaultSvgHeight)
      .append("g")
      .attr("transform", "translate(" + margin.left + "," + margin.top + ")");

    this.xAxis = this.svg.append("g")
      .attr("class", "x-axis")
      .attr("transform", "translate(0," + (this.state.height - graphPadding) + ")");

    this.yAxis = this.svg.append("g")
      .attr("class", "y-axis")
      .attr("transform", "translate(" + this.state.width + ",0)");

    this.sidebar = d3.select(".scatterplot-display")
      .append("div").attr("class", "sidebar-tooltip");

    this.initializeVerticalHighlight();
    this.renderAxisLabels();
    this.updateGraph();
  }

  shouldComponentUpdate(nextProps) {
    if (nextProps.lastUpdated === this.props.lastUpdated) {
      // control whether react re-renders the component
      // only rerender if the input data has changed
      return false;
    }
    return true;
  }

  componentDidUpdate() {
    this.updateGraph();
  }

  initializeVerticalHighlight() {
    this.updateScales(this.props.data);

    // highlight bar to show x position
    this.verticalHighlight = this.svg.append("rect")
      .attr("class", "vertical-highlight")
      .attr("width", highlightBarWidth)
      .attr("height", this.state.height);

    // overlay on which to attach mouse events
    // attach this after all other items are attached otherwise they block mouse events
    this.overlay = this.svg.append("rect")
      .attr("transform", "translate(" + margin.left + "," + margin.top + ")")
      .attr("class", "overlay")
      .attr("width", this.state.width)
      .attr("height", this.state.height);
    this.overlayNode = d3.select(".overlay").node();

    this.overlayTooltip = this.svg.append("g")
      .attr("class", "overlay-tooltip")
      .attr("height", this.state.height);

    this.highlightFirstDatapoint();
  }

  highlightFirstDatapoint() {
    // when graph is initially loaded / reloaded, set highlight and sidebar to first datapoint
    let firstDatapoint = _.first(this.props.data);
    if (firstDatapoint) {
      let firstLatency = _.get(firstDatapoint, ["latency", "P99"]);
      let firstLatencyX = this.xScale(firstLatency);
      let nearestDatapoints = this.getNearbyDatapoints(firstLatencyX, this.props.data);

      this.verticalHighlight
        .attr("transform", "translate(" + (firstLatencyX - (highlightBarWidth/2)) + ", 0)");
      this.renderSidebarTooltip(nearestDatapoints);
      this.overlayTooltip.text('');
    }
  }

  getChartDimensions() {
    let svgWidth = this.props.width || defaultSvgWidth;
    let svgHeight = this.props.height || defaultSvgHeight;

    let width = svgWidth - margin.left - margin.right;
    let height = svgHeight - margin.top - margin.bottom;

    return {
      svgWidth: svgWidth,
      svgHeight: svgHeight,
      width: width,
      height: height,
      margin: margin
    };
  }

  initializeScales() {
    this.xScale = d3.scaleLinear().range([graphPadding, this.state.width - graphPadding]);
    this.yScale = d3.scaleLinear().range([this.state.height - graphPadding, graphPadding]);
  }

  updateScales(data) {
    this.xScale.domain(d3.extent(data, d => d.latency.P99));
    this.yScale.domain([0, 1]);

    this.updateAxes();
  }

  updateAxes() {
    let xAxis = d3.axisBottom(this.xScale)
      .ticks(5)
      .tickSize(5);
    this.xAxis.call(xAxis);

    let yAxis = d3.axisLeft(this.yScale)
      .ticks(4)
      .tickSize(this.state.width)
      .tickFormat(metricToFormatter["SUCCESS_RATE"]);

    // custom axis styling: https://bl.ocks.org/mbostock/3371592
    let customYAxis = g => {
      g.call(yAxis);
      g.select(".domain").remove();
      g.selectAll(".tick text")
        .attr("x", 4)
        .attr("dx", -10)
        .attr("dy", -4);
    };
    this.yAxis.call(customYAxis);
  }

  renderAxisLabels() {
    // text label for the x axis
    this.svg.append("text")
      .attr("class", "axis-label x-axis-label")
      .attr("y", this.state.height - 12)
      .attr("x", 0)
      .text("p99 Latency (ms)");

    // text label for the y axis
    this.svg.append("text")
      .attr("class", "axis-label y-axis-label")
      .attr("y", 20)
      .attr("x", this.state.width)
      .attr("dx", "-3em")
      .text("Success rate");
  }

  getNearbyDatapoints(x, data) {
    // return nodes that have nearby x-coordinates
    let x0 = this.xScale.invert(x - highlightBarWidth);
    let x1 = this.xScale.invert(x + highlightBarWidth);

    if (x0 === x1) {
      // handle case where all the x points are in one column
      let datapointsX = this.xScale(_.first(data).latency.P99);
      if (Math.abs(x - datapointsX < highlightBarWidth)) {
        return data;
      } else {
        return [];
      }
    } else {
      return _(data).filter(d => {
        return d.latency.P99 <= x1 && d.latency.P99 >= x0;
      }).orderBy('successRate', 'desc').value();
    }
  }

  updateGraph() {
    this.updateScales(this.props.data);

    this.scatterPlot = this.svg.selectAll(".dot")
      .data(this.props.data);

    this.scatterPlot.exit().remove();

    let spNode = this.scatterPlot.node();

    this.scatterPlot
      .enter()
      .append("circle")
      .attr("class", "dot")
      .attr("r", circleRadius)
      .merge(this.scatterPlot) // newfangled d3 'update' selection
      .attr("cx", d => this.xScale(d.latency.P99))
      .attr("cy", d => this.yScale(d.successRate))
      .style("fill", d => successRateColorScale(d.successRate))
      .style("stroke", d => successRateColorScale(d.successRate))
      .on("mousemove", () => {
        if (spNode) {
          let currXPos = d3.mouse(spNode)[0];
          this.positionOverlayHighlightAndTooltip(currXPos);
        }
      });

    this.highlightFirstDatapoint();
    this.overlay
      .on("mousemove", () => {
        let currXPos = d3.mouse(this.overlayNode)[0];
        this.positionOverlayHighlightAndTooltip(currXPos);
      });
  }

  positionOverlayHighlightAndTooltip(currXPos) {
    let nearestDatapoints = this.getNearbyDatapoints(currXPos, this.props.data);
    this.renderOverlayTooltip(nearestDatapoints);
    this.renderSidebarTooltip(nearestDatapoints);
    this.verticalHighlight.attr("transform", "translate(" + (currXPos - highlightBarWidth / 2) + ", 0)");

    let bbox = this.overlayTooltip.node().getBBox();

    let overlayTooltipYPos = 0;
    let firstLabelPosition = _.isEmpty(nearestDatapoints) ? null : this.getTooltipLabelY(nearestDatapoints[0]);
    if (firstLabelPosition + bbox.height > this.state.height - 50) {
      // if there are a bunch of nodes at 0, the labels could extend below the chart
      // translate upward if this is the case
      overlayTooltipYPos -= (bbox.height);
    }

    let overlayTooltipXPos = currXPos + highlightBarWidth / 2 + baseWidth;
    if (currXPos > defaultSvgWidth / 2) {
      // display tooltip to the left if we're on the RH side of the graph
      overlayTooltipXPos = currXPos - bbox.width - baseWidth - highlightBarWidth / 2;
    }

    this.overlayTooltip
      .attr("transform", "translate(" + overlayTooltipXPos + ", " + overlayTooltipYPos + ")")
      .raise();
  }

  renderSidebarTooltip(data) {
    let innerHtml = _.map(data, d => this.renderNodeTooltipDatum(d));
    this.sidebar.html(innerHtml.join(''));
  }

  renderOverlayTooltip(data) {
    this.overlayTooltip.text('');
    let labelWithPosition = this.computeTooltipLabelPositions(data);

    _.each(labelWithPosition, d => {
      this.overlayTooltip
        .append("text").text(d.name)
        .attr("x", 0)
        .attr("y", d.computedY);
    });
  }

  getTooltipLabelY(datum) {
    // position the tooltip label roughly aligned with the center of the node
    return this.yScale(datum.successRate) + circleRadius / 2 - 5;
  }

  computeTooltipLabelPositions(data) {
    // in the case that there are multiple nodes in the highlighted area,
    // try to position each label next to its corresponding node
    // if the nodes are too close together, simply list the node labels
    let positions = _.map(data, d => {
      return {
        name: d.name,
        computedY: this.getTooltipLabelY(d)
      };
    });

    if (_.size(positions) > 1) {
      let shouldAutoSpace = false;
      _.each(positions, (_d, i) => {
        if (i > 0) {
          if (positions[i].computedY - positions[i - 1].computedY < 10) {
            shouldAutoSpace = true;
          }
        }
      });
      if (shouldAutoSpace) {
        let basePos = positions[0].computedY;
        _.each(positions, (d, i) => {
          d.computedY = basePos + i * 15;
        });
      }
    }
    return positions;
  }

  renderNodeTooltipDatum(d) {
    let latency = metricToFormatter["LATENCY"](d.latency.P99);
    let sr = metricToFormatter["SUCCESS_RATE"](d.successRate);
    return `<div class="title">${d.name}</div><div>${latency}, ${sr}</div>`;
  }

  render() {
    // d3 selects the passed in container from this.props.containerClassName
    return _.isEmpty(this.props.data) ?
      <div className="clearfix no-data-msg">No data</div> : null;
  }
}
