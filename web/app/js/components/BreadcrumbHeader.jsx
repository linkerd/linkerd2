import _ from 'lodash';
import PropTypes from "prop-types";
import React from 'react';
import ReactRouterPropTypes from 'react-router-prop-types';
import { withContext } from './util/AppContext.jsx';
import { Breadcrumb, Layout } from 'antd';
import { friendlyTitle, isResource, singularResource } from "./util/Utils.js";
import './../../css/breadcrumb-header.css';


const routeToCrumbTitle = {
  "servicemesh": "Service Mesh",
  "replicationcontrollers": "Replication Controllers",
  "tap": "Tap",
  "top": "Top"
};

class BreadcrumbHeader extends React.Component {

  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired,
    location: ReactRouterPropTypes.location.isRequired
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;
  }

  processResourceDetailURL(segments) {
    if (segments.length === 4) {
      let splitSegments = _.chunk(segments, 2);
      let resourceNameSegment = splitSegments[1];
      resourceNameSegment[0] = singularResource(resourceNameSegment[0]);
      return _.concat(splitSegments[0], resourceNameSegment.join('/'));
    } else {
      return segments;
    }
  }

  convertURLToBreadcrumbs(location) {
    if (location.length === 0) {
      return [];
    } else {
      let segments = location.split('/').slice(1);
      let finalSegments = this.processResourceDetailURL(segments);

      return _.map(finalSegments, segment => {
        let partialUrl = _.takeWhile(segments, s => {
          return s !== segment;
        });

        if (partialUrl.length !== segments.length) {
          partialUrl.push(segment);
        }

        return {
          link: `/${partialUrl.join("/")}`,
          segment: segment
        };
      });
    }
  }

  renderBreadcrumbSegment(segment) {
    console.log(segment);
    let isMeshResource = isResource(segment);

    if (isMeshResource) {
      return routeToCrumbTitle[segment] || friendlyTitle(segment).singular;
    }

    console.log(routeToCrumbTitle[segment]);
    return routeToCrumbTitle[segment] || segment;
  }

  render() {
    let PrefixedLink = this.api.PrefixedLink;
    let breadcrumbs = this.convertURLToBreadcrumbs(this.props.location.pathname);

    return (
      <Layout.Header>
        <Breadcrumb separator=">">
          {
            _.map(breadcrumbs, pathSegment => {
              return (
                <Breadcrumb.Item key={pathSegment.segment}>
                  <PrefixedLink
                    to={pathSegment.link}>
                    {this.renderBreadcrumbSegment(pathSegment.segment)}
                  </PrefixedLink>
                </Breadcrumb.Item>
              );
            })
          }
        </Breadcrumb>
      </Layout.Header>
    );
  }
}

export default withContext(BreadcrumbHeader);
