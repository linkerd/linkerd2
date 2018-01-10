import _ from 'lodash';
import React from 'react';
import * as d3 from 'd3';
import './../../css/line-graph.css';

const defaultSvgWidth = 238;
const defaultSvgHeight = 72;
const margin = { top: 6, right: 6, bottom: 6, left: 0 };

export default class LineGraph extends React.Component {
  constructor(props) {
    super(props);

    this.state = this.getChartDimensions();
  }

  componentWillMount() {
    this.initializeScales();
  }

  componentDidMount() {
    this.svg = d3.select("." + this.props.containerClassName)
      .append("svg")
      .attr("width", this.state.svgWidth)
      .attr("height", this.state.svgHeight)
      .append("g")
      .attr("transform", "translate(" + this.state.margin.left + "," + this.state.margin.top + ")");
    this.xAxis = this.svg.append("g")
      .attr("transform", "translate(0," + this.state.height + ")");
    this.yAxis = this.svg.append("g");

    this.loadingMessage = this.svg
      .append("text")
      .attr("transform",
        "translate(" + (this.state.width / 2 - 30) + "," + (this.state.height / 2) + ")");

    this.updateScales();
    this.initializeGraph();
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
    this.updateScales();
    this.updateGraph();
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

  updateScales() {
    let data = this.props.data;
    let ymax = d3.max(data, d => parseFloat(d.value));
    let padding = 0;
    if (this.state.svgHeight) {
      padding = ymax / this.state.svgHeight;
    }
    this.xScale.domain(d3.extent(data, d => parseInt(d.timestamp)));
    this.yScale.domain([0-padding, ymax+padding]);
  }

  initializeScales() {
    this.xScale = d3.scaleLinear().range([0, this.state.width]);
    this.yScale = d3.scaleLinear().range([this.state.height, 0]);

    let x = this.xScale;
    let y = this.yScale;

    // define the line
    this.line = d3.line()
      .x(d => x(d.timestamp))
      .y(d => y(d.value));
  }

  initializeGraph() {
    if (_.isEmpty(this.props.data)) {
      this.loadingMessage.text("---");
    }

    this.svg.select("path").remove();

    let lineChart = this.svg.append("path")
      .attr("class", "chart-line line");

    lineChart
      .attr("d", this.line(this.props.data));

    this.svg.append("circle")
      .attr("class", "flash")
      .attr("flashing", "off")
      .style("opacity", 0)
      .attr("r", 6);

    this.updateAxes();
    this.flashLatestDataPoint();
  }

  updateGraph() {
    if (_.isEmpty(this.props.data)) {
      this.loadingMessage.style("opacity", 1);
    } else {
      this.loadingMessage.style("opacity", 0);
    }

    this.svg.select(".line")
      .transition()
      .duration(450)
      .attr("d", this.line(this.props.data));

    this.updateAxes();
    this.flashLatestDataPoint();
  }

  updateAxes() {
    if (this.props.showAxes) {
      this.xAxis
        .call(d3.axisBottom(this.xScale)); // add x axis labels

      this.yAxis
        .call(d3.axisLeft(this.yScale)); // add y axis labels
    }
  }

  flashLatestDataPoint() {
    if (this.props.flashLastDatapoint === false) {
      return;
    }

    let circle = this.svg.select("circle");
    if (_.isEmpty(this.props.data)) {
      circle.attr("flashing", "off").interrupt().style("opacity", 0);
    } else {
      if (circle.attr("flashing") === "off") {
        circle
          .attr("flashing", "on")
          .transition()
          .on("start", function repeat() {
            d3.active(this)
              .transition()
              .duration(1000)
              .style("opacity", 0.6)
              .transition()
              .duration(1000)
              .style("opacity", 0)
              .transition()
              .on("start", repeat);
          });
      }

      let circleData = _.last(this.props.data);
      circle
        .attr("cx", () => {
          return this.xScale(circleData.timestamp);
        })
        .attr("cy", () => {
          return this.yScale(circleData.value);
        });
    }
  }

  render() {
    return (
      <div className={`line-graph ${this.props.containerClassName}`} />
    );
  }
}
